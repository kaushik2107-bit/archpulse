package analysis

import (
	"strings"
	"testing"

	"archpulse/internal/ir"
	"archpulse/internal/kernel"
	"archpulse/internal/metrics"
)

func TestAnalyzeRanksEarlierDownstreamConstraintAboveUpstreamSymptom(t *testing.T) {
	sink := testSink(2)
	for second := 0; second < 12; second++ {
		at := kernel.SimTime(second) * kernel.Second
		upQueue := 0.0
		if second >= 4 {
			upQueue = float64((second - 3) * 10)
		}
		downQueue := 0.0
		if second >= 2 {
			downQueue = float64((second - 1) * 20)
		}
		recordPressure(sink, 0, at, upQueue, 95)
		recordPressure(sink, 1, at, downQueue, 100)
		rate := uint64(100)
		if second >= 7 {
			rate = 20
		}
		sink.Global().Throughput.IncrementBy(at, rate)
	}
	graph := &ir.Graph{
		Nodes: []ir.Node{{ID: "caller"}, {ID: "dependency"}},
		Edges: []ir.Edge{{From: "caller", To: "dependency"}},
	}

	report := AnalyzeWithGraph(kernel.RunTrace{}, sink, graph)
	if len(report.Ranked) != 2 {
		t.Fatalf("got %d verdicts", len(report.Ranked))
	}
	if report.Ranked[0].Resource != 1 || report.Ranked[0].Classification != "root_constraint" {
		t.Fatalf("expected downstream resource first as root constraint, got %+v", report.Ranked)
	}
	if report.Ranked[1].Classification != "upstream_symptom" || !strings.Contains(report.Ranked[1].Reason, "backpressure") {
		t.Fatalf("expected upstream symptom explanation, got %+v", report.Ranked[1])
	}
	if len(report.ThroughputDrops) == 0 || report.ThroughputDrops[0].DropPct < 30 {
		t.Fatalf("expected material throughput drop, got %+v", report.ThroughputDrops)
	}
}

func TestAnalyzeDoesNotFlagBriefQueueSpike(t *testing.T) {
	sink := testSink(1)
	for second := 0; second < 10; second++ {
		queue := 0.0
		if second == 4 {
			queue = 50
		}
		recordPressure(sink, 0, kernel.SimTime(second)*kernel.Second, queue, 100)
	}
	report := Analyze(kernel.RunTrace{}, sink)
	if report.Ranked[0].IsBottleneck || report.Ranked[0].Classification != "transient_pressure" {
		t.Fatalf("brief spike should be transient pressure, got %+v", report.Ranked[0])
	}
}

func testSink(resourceCount int) *metrics.Sink {
	world := &kernel.World{}
	for range resourceCount {
		world.Reserve()
	}
	return metrics.NewSink(world, 0, kernel.Second)
}

func recordPressure(sink *metrics.Sink, id kernel.ResourceID, at kernel.SimTime, queue, utilization float64) {
	m := sink.PerResource()[id]
	m.QueueDepth.Record(at, queue)
	m.Utilization.Record(at, utilization)
}
