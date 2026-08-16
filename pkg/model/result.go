package model

import (
	"infra-sim/internal/analysis"
	"infra-sim/internal/kernel"
	"infra-sim/internal/metrics"
)

type LatencySummary struct {
	Count uint64  `json:"count"`
	Mean  float64 `json:"mean_us"`
	P50   float64 `json:"p50_us"`
	P95   float64 `json:"p95_us"`
	P99   float64 `json:"p99_us"`
}

type RunResult struct {
	Seed       int64                     `json:"seed"`
	Trace      kernel.RunTrace           `json:"trace"`
	Throughput []metrics.Point           `json:"throughput_rps"`
	Latency    LatencySummary            `json:"latency"`
	Rejected   uint64                    `json:"rejected"`
	Bottleneck analysis.BottleneckReport `json:"bottleneck"`
}

func NewRunResult(seed int64, trace kernel.RunTrace, sink *metrics.Sink, bottleneck analysis.BottleneckReport) RunResult {
	histogram := sink.Global().Latency
	return RunResult{
		Seed:       seed,
		Trace:      trace,
		Throughput: sink.Global().Throughput.Points(),
		Latency: LatencySummary{
			Count: histogram.Count(), Mean: histogram.Mean(), P50: histogram.Quantile(50), P95: histogram.Quantile(95), P99: histogram.Quantile(99),
		},
		Rejected:   sink.Global().Rejected,
		Bottleneck: bottleneck,
	}
}
