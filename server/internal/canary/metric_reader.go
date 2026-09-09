package canary

import (
	"context"
	"strings"

	"github.com/NeoPlayful/maple-gateway/server/internal/metrics"
)

const (
	counterRequests = "maple_requests_total"
	histoDuration   = "maple_request_duration_seconds"
)

// versionMetricReader 基于 metrics.TimeSeries 时间桶读某 deployment version 的窗口指标。
// labels 编码 "k=v,k=v"；proxy 埋点已带 version=<id>。
type versionMetricReader struct {
	series *metrics.TimeSeries
	window int // 聚合最近 N 个窗口
}

// NewVersionMetricReader 构造。series 为空时返回 nil。
func NewVersionMetricReader(series *metrics.TimeSeries) MetricReader {
	if series == nil {
		return nil
	}
	return &versionMetricReader{series: series, window: 2}
}

// Report 聚合最近若干窗口内 versionID 的请求/错误/平均延迟。
// 窗口内完全无该版本请求返回 ok=false（样本不足）。
func (r *versionMetricReader) Report(_ context.Context, versionID string) (VersionStat, bool) {
	pts := r.series.Range(r.window)
	if len(pts) == 0 {
		return VersionStat{}, false
	}

	var req, errs int64
	var sumMS float64
	var count int64
	for _, p := range pts {
		for labels, v := range p.Counters[counterRequests] {
			if !labelHasVersion(labels, versionID) {
				continue
			}
			req += v
			if isErrLabels(labels) {
				errs += v
			}
		}
		for labels, hs := range p.Histograms[histoDuration] {
			if !labelHasVersion(labels, versionID) {
				continue
			}
			sumMS += hs.Sum * 1000
			count += hs.Count
		}
	}
	if req == 0 {
		return VersionStat{}, false
	}
	st := VersionStat{Requests: req, Errors: errs}
	if count > 0 {
		st.AvgMS = sumMS / float64(count)
	}
	return st, true
}

// labelHasVersion 判定编码后的 labels 是否含指定 version=<id>。
func labelHasVersion(labels, versionID string) bool {
	if labels == "" {
		return false
	}
	for _, part := range strings.Split(labels, ",") {
		if strings.HasPrefix(part, "version=") {
			return strings.TrimPrefix(part, "version=") == versionID
		}
	}
	return false
}

// isErrLabels 判定 labels 是否错误维度：status=5xx 或 status=rejected。
func isErrLabels(labels string) bool {
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
