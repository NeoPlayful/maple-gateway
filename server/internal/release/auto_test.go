package release

import "testing"

func TestShouldStopLoss_ErrorRate(t *testing.T) {
	a := &Auto{cfg: AutoConfig{ErrRateMax: 5, MinRequests: 10}}
	if a.shouldStopLoss(VersionStat{Requests: 100, Errors: 3}) {
		t.Fatal("3% errors should not stop-loss")
	}
	if !a.shouldStopLoss(VersionStat{Requests: 100, Errors: 8}) {
		t.Fatal("8% errors should stop-loss")
	}
}

func TestShouldStopLoss_ZeroRequests(t *testing.T) {
	a := &Auto{cfg: AutoConfig{ErrRateMax: 5}}
	// 0 请求时错误率为 0，不触发止损（样本不足在 evaluateRelease 已拦截）。
	if a.shouldStopLoss(VersionStat{Requests: 0, Errors: 0}) {
		t.Fatal("zero requests should not stop-loss")
	}
}

func TestShouldStopLoss_Latency(t *testing.T) {
	// 延迟阈值未启用（=0）：高延迟不触发。
	a := &Auto{cfg: AutoConfig{ErrRateMax: 5, ErrLatencyMS: 0}}
	if a.shouldStopLoss(VersionStat{Requests: 100, AvgMS: 9999}) {
		t.Fatal("latency disabled should not stop-loss on high latency")
	}
	// 延迟阈值启用：超限触发。
	b := &Auto{cfg: AutoConfig{ErrRateMax: 5, ErrLatencyMS: 500}}
	if !b.shouldStopLoss(VersionStat{Requests: 100, AvgMS: 900}) {
		t.Fatal("high latency should stop-loss when enabled")
	}
	if b.shouldStopLoss(VersionStat{Requests: 100, AvgMS: 200}) {
		t.Fatal("normal latency should not stop-loss")
	}
}

func TestLabelHasVersion(t *testing.T) {
	cases := []struct {
		labels  string
		version string
		want    bool
	}{
		{"host=a.com,status=200,version=abc", "abc", true},
		{"host=a.com,status=200,version=abc", "def", false},
		{"host=a.com,status=500,version=abc", "abc", true},
		{"host=a.com,status=200", "abc", false}, // 直挂无 version
		{"", "abc", false},
	}
	for _, c := range cases {
		if got := labelHasVersion(c.labels, c.version); got != c.want {
			t.Fatalf("labelHasVersion(%q, %q) = %v, want %v", c.labels, c.version, got, c.want)
		}
	}
}

func TestIsErrLabels(t *testing.T) {
	if !isErrLabels("host=a.com,status=500,version=abc") {
		t.Fatal("500 should be error")
	}
	if isErrLabels("host=a.com,status=200,version=abc") {
		t.Fatal("200 should not be error")
	}
	if !isErrLabels("host=a.com,status=rejected") {
		t.Fatal("rejected should be error")
	}
	if isErrLabels("host=a.com,status=404") {
		t.Fatal("404 should not be error")
	}
}

func TestNewVersionMetricReader_NilSeries(t *testing.T) {
	// 无指标时间桶时不启动执行器（返回 nil）。
	if r := NewVersionMetricReader(nil); r != nil {
		t.Fatalf("nil series should yield nil reader, got %v", r)
	}
}
