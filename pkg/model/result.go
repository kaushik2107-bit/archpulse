package model

import (
	"archpulse/internal/analysis"
	"archpulse/internal/ir"
	"archpulse/internal/kernel"
	"archpulse/internal/metrics"
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
	Resources         []ResourceDescriptor      `json:"resources,omitempty"`
	SamplingFactor    int                       `json:"sampling_factor"`
	EstimatedArrivals uint64                    `json:"estimated_arrivals"`
}

type ResourceDescriptor struct {
	ResourceID kernel.ResourceID `json:"resource_id"`
	NodeID     ir.NodeID         `json:"node_id"`
	Name       string            `json:"name,omitempty"`
	Type       string            `json:"type"`
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

func NewRunResult(seed int64, trace kernel.RunTrace, sink *metrics.Sink, bottleneck analysis.BottleneckReport, graph *ir.Graph, samplingFactor int, estimatedArrivals uint64) RunResult {
	histogram := sink.Global().Latency
	result := RunResult{
		Seed:       seed,
		Trace:      trace,
		Throughput: sink.Global().Throughput.Points(),
		Latency: LatencySummary{
			Count: histogram.Count(), Mean: histogram.Mean(), P50: histogram.Quantile(50), P95: histogram.Quantile(95), P99: histogram.Quantile(99),
		},
		Rejected:       sink.Global().Rejected,
		Bottleneck:     bottleneck,
		SamplingFactor: max(1, samplingFactor), EstimatedArrivals: estimatedArrivals,
	}
	if graph != nil {
		for index, node := range graph.Nodes {
			result.Resources = append(result.Resources, ResourceDescriptor{ResourceID: kernel.ResourceID(index), NodeID: node.ID, Name: node.Name, Type: node.ResourceType})
		}
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
