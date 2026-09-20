package gateway

import (
	"runtime"
	"runtime/metrics"
	"time"
)

// threadSample 复用同一份采样描述，避免每轮分配。
var threadSamples = []metrics.Sample{
	{Name: "/sched/threads/total:threads"},
}

// numThreads 返回 Go 运行时当前管理的 OS 线程数。
// runtime 未直接导出该值，经 runtime/metrics 的 /sched/threads/total 采样。
func numThreads() int64 {
	metrics.Read(threadSamples)
	if threadSamples[0].Value.Kind() == metrics.KindUint64 {
		return int64(threadSamples[0].Value.Uint64())
	}
	return 0
}

// startResourceGauges 周期采集进程资源水位到指标表，供过载前告警。
//
// 关键先行指标是 OS 线程数：Windows 上每条阻塞 socket 占一个线程，线程数逼近
// 10000 会触发运行时 fatal——在崩之前看到它爬升，就有时间介入。goroutine 数与
// 数据面在途数是上游连接扩张的另外两个先兆。
func (d *DataPlane) startResourceGauges() {
	if d.metrics == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			d.metrics.SetGauge("maple_goroutines", int64(runtime.NumGoroutine()), nil)
			d.metrics.SetGauge("maple_os_threads", numThreads(), nil)
			if d.inflight != nil {
				d.metrics.SetGauge("maple_inflight_requests", d.inflight.InFlight(), nil)
			}
		}
	}()
}
