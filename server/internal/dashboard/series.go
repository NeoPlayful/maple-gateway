// Dashboard 时序聚合：从 metrics 环形时间桶读窗口增量，汇总为趋势序列。
package dashboard

import (
	"sort"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
)

// TrafficPoint 一个时间窗口的流量/错误汇总。
type TrafficPoint struct {
	Time          int64   `json:"time"` // Unix 秒
	Requests      int64   `json:"requests"`
	Errors        int64   `json:"errors"`          // 5xx + rejected
	RateLimitHits int64   `json:"rate_limit_hits"` // 限流命中
	RPS           float64 `json:"rps"`             // 窗口内每秒请求
	ErrorRate     float64 `json:"error_rate"`      // 0-100
}

// LatencyPoint 一个时间窗口的延迟汇总。
type LatencyPoint struct {
	Time  int64   `json:"time"` // Unix 秒
	Count int64   `json:"count"`
	AvgMS float64 `json:"avg_ms"` // 平均延迟（毫秒）
	P95MS float64 `json:"p95_ms"` // 近似 p95 延迟（毫秒）
}

const (
	counterRequests  = "maple_requests_total"
	counterRatelimit = "maple_rate_limit_hits_total"
	histoDuration    = "maple_request_duration_seconds"
)

// SeriesReader 抽象时间桶读取，便于注入与测试。
type SeriesReader interface {
	Range(n int) []*metrics.WindowPoint
}

// Traffic 读时间桶最近 n 个窗口的请求/错误/限流命中序列。
// 空 series 或尚无窗口时返回空数组。
func Traffic(series SeriesReader, n int) []TrafficPoint {
	if series == nil {
		return []TrafficPoint{}
	}
	pts := series.Range(n)
	out := make([]TrafficPoint, 0, len(pts))
	for _, p := range pts {
		requests := sumLabels(p.Counters[counterRequests], nil)
		tp := TrafficPoint{
			Time:          p.Time,
			RateLimitHits: sumLabels(p.Counters[counterRatelimit], nil),
			Requests:      requests,
			Errors:        sumLabels(p.Counters[counterRequests], isErrLabel),
		}
		if secs := windowSeconds(pts, p.Time); secs > 0 {
			tp.RPS = float64(tp.Requests) / secs
		}
		if tp.Requests > 0 {
			tp.ErrorRate = float64(tp.Errors) / float64(tp.Requests) * 100
		}
		out = append(out, tp)
	}
	return out
}

// Latency 读时间桶 histogram 差分，汇总各窗口平均与 p95 延迟。
func Latency(series SeriesReader, n int) []LatencyPoint {
	if series == nil {
		return []LatencyPoint{}
	}
	pts := series.Range(n)
	out := make([]LatencyPoint, 0, len(pts))
	for _, p := range pts {
		lp := LatencyPoint{Time: p.Time}
		var sums float64
		var samples []float64
		for _, hs := range p.Histograms[histoDuration] {
			if hs.Count <= 0 {
				continue
			}
			lp.Count += hs.Count
			sums += hs.Sum
			// 单序列样本 = 均值；p95 用各序列均值近似。
			samples = append(samples, hs.Sum/float64(hs.Count))
		}
		if lp.Count > 0 {
			lp.AvgMS = sums / float64(lp.Count) * 1000
			lp.P95MS = percentileMS(samples, 0.95)
		}
		out = append(out, lp)
	}
	return out
}

// windowSeconds 推算相邻窗口间隔（秒）；旧窗口差分不可靠时用邻居，兜底按默认 15s。
func windowSeconds(pts []*metrics.WindowPoint, t int64) float64 {
	if len(pts) >= 2 {
		// 序列是最新在前；用相邻两个 Time 的差作为窗口宽。
		if pts[0].Time != pts[1].Time {
			secs := float64(pts[0].Time - pts[1].Time)
			if secs > 0 && secs <= 3600 {
				return secs
			}
		}
	}
	return 15
}

// sumLabels 汇总 counter 的标签增量；filter 为 nil 则全计。
func sumLabels(m map[string]int64, filter func(labels string) bool) int64 {
	var total int64
	for labels, v := range m {
		if filter == nil || filter(labels) {
			total += v
		}
	}
	return total
}

// isErrLabel 判定 labels 是否为错误维度：status=5xx 或 status=rejected。
func isErrLabel(labels string) bool {
	for _, part := range strings.Split(labels, ",") {
		if !strings.HasPrefix(part, "status=") {
			continue
		}
		code := strings.TrimPrefix(part, "status=")
		if code == "rejected" {
			return true
		}
		if len(code) == 3 && code[0] == '5' {
			return true
		}
	}
	return false
}

// percentileMS 对样本求分位（毫秒）。样本为空返回 0。
func percentileMS(samples []float64, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	idx := int(p*float64(len(sorted)) + 0.5)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx] * 1000
}
