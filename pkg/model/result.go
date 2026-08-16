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
	Seed              int64                     `json:"seed"`
	Trace             kernel.RunTrace           `json:"trace"`
	Throughput        []metrics.Point           `json:"throughput_rps"`
	Latency           LatencySummary            `json:"latency"`
	Rejected          uint64                    `json:"rejected"`
	Bottleneck        analysis.BottleneckReport `json:"bottleneck"`
	ResourceTimelines []ResourceTimeline        `json:"resource_timelines"`
}

type ResourceTimeline struct {
	ResourceID  kernel.ResourceID  `json:"resource_id"`
	Utilization []metrics.Point    `json:"utilization_pct"`
	QueueDepth  []metrics.Point    `json:"queue_depth"`
	Instances   []InstanceTimeline `json:"instances,omitempty"`
}

type InstanceTimeline struct {
	Instance    int             `json:"instance"`
	Utilization []metrics.Point `json:"utilization_pct"`
	QueueDepth  []metrics.Point `json:"queue_depth"`
	Degraded    []metrics.Point `json:"degraded"`
}

func NewRunResult(seed int64, trace kernel.RunTrace, sink *metrics.Sink, bottleneck analysis.BottleneckReport) RunResult {
	histogram := sink.Global().Latency
	result := RunResult{
		Seed:       seed,
		Trace:      trace,
		Throughput: sink.Global().Throughput.Points(),
		Latency: LatencySummary{
			Count: histogram.Count(), Mean: histogram.Mean(), P50: histogram.Quantile(50), P95: histogram.Quantile(95), P99: histogram.Quantile(99),
		},
		Rejected:   sink.Global().Rejected,
		Bottleneck: bottleneck,
	}
	for id := kernel.ResourceID(0); int(id) < len(sink.PerResource()); id++ {
		resource := sink.PerResource()[id]
		timeline := ResourceTimeline{
			ResourceID: id, Utilization: resource.Utilization.Points, QueueDepth: resource.QueueDepth.Points,
		}
		for instance := 1; instance <= len(sink.PerInstance()[id]); instance++ {
			member := sink.PerInstance()[id][instance]
			timeline.Instances = append(timeline.Instances, InstanceTimeline{Instance: instance, Utilization: member.Utilization.Points, QueueDepth: member.QueueDepth.Points, Degraded: member.Degraded.Points})
		}
		result.ResourceTimelines = append(result.ResourceTimelines, timeline)
	}
	return result
}
