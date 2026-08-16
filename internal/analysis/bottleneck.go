package analysis

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"archpulse/internal/ir"
	"archpulse/internal/kernel"
	"archpulse/internal/metrics"
)

type BottleneckReport struct {
	PlateauStartTime  kernel.SimTime    `json:"plateau_start_time_ns"`
	PlateauThroughput float64           `json:"plateau_throughput_rps"`
	ThroughputDrops   []ThroughputDrop  `json:"throughput_drops,omitempty"`
	Ranked            []ResourceVerdict `json:"ranked_resources"`
}

type ThroughputDrop struct {
	Time      kernel.SimTime `json:"time_ns"`
	BeforeRPS float64        `json:"before_rps"`
	AfterRPS  float64        `json:"after_rps"`
	DropPct   float64        `json:"drop_pct"`
}

type ResourceVerdict struct {
	Resource              kernel.ResourceID `json:"resource_id"`
	UtilizationPct        float64           `json:"utilization_pct"`
	QueueDepth            float64           `json:"queue_depth"`
	QueueSlope            float64           `json:"queue_slope_per_sec"`
	PeakUtilizationPct    float64           `json:"peak_utilization_pct"`
	MaxQueueDepth         float64           `json:"max_queue_depth"`
	FinalQueueDepth       float64           `json:"final_queue_depth"`
	SaturationFractionPct float64           `json:"saturation_fraction_pct"`
	FirstQueuedTime       kernel.SimTime    `json:"first_queued_time_ns,omitempty"`
	Score                 float64           `json:"score"`
	Classification        string            `json:"classification"`
	IsBottleneck          bool              `json:"is_bottleneck"`
	Reason                string            `json:"reason"`
	queuedSamples         int
	queueSamples          int
}

func Analyze(trace kernel.RunTrace, sink *metrics.Sink) BottleneckReport {
	return AnalyzeWithGraph(trace, sink, nil)
}

// AnalyzeWithGraph uses evidence from the entire run. Topology lets it separate
// a downstream constraint from backpressure propagated to upstream callers.
func AnalyzeWithGraph(_ kernel.RunTrace, sink *metrics.Sink, graph *ir.Graph) BottleneckReport {
	throughput := sink.Global().Throughput.Points()
	plateauTime, plateauThroughput := detectPlateau(throughput)
	ids := make([]kernel.ResourceID, 0, len(sink.PerResource()))
	for id := range sink.PerResource() {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	verdicts := make([]ResourceVerdict, 0, len(ids))
	for _, id := range ids {
		verdicts = append(verdicts, evaluateResource(id, sink.PerResource()[id], plateauTime))
	}
	applyCausality(verdicts, reachability(graph))
	for i := range verdicts {
		finalizeReason(&verdicts[i])
	}
	sort.SliceStable(verdicts, func(i, j int) bool {
		if verdicts[i].IsBottleneck != verdicts[j].IsBottleneck {
			return verdicts[i].IsBottleneck
		}
		if verdicts[i].Score != verdicts[j].Score {
			return verdicts[i].Score > verdicts[j].Score
		}
		return verdicts[i].Resource < verdicts[j].Resource
	})
	return BottleneckReport{plateauTime, plateauThroughput, detectThroughputDrops(throughput), verdicts}
}

func evaluateResource(id kernel.ResourceID, m *metrics.ResourceMetrics, plateau kernel.SimTime) ResourceVerdict {
	v := ResourceVerdict{Resource: id, Classification: "healthy", queueSamples: len(m.QueueDepth.Points)}
	if len(m.QueueDepth.Points) > 0 {
		v.QueueDepth = m.QueueDepth.ValueAt(plateau)
		v.FinalQueueDepth = m.QueueDepth.Points[len(m.QueueDepth.Points)-1].Value
	}
	for i, p := range m.QueueDepth.Points {
		v.MaxQueueDepth = math.Max(v.MaxQueueDepth, p.Value)
		if p.Value > 0 {
			v.queuedSamples++
			if v.queuedSamples == 1 {
				v.FirstQueuedTime = p.Time
			}
		}
		if i > 0 {
			seconds := float64(p.Time-m.QueueDepth.Points[i-1].Time) / float64(kernel.Second)
			if seconds > 0 {
				v.QueueSlope = math.Max(v.QueueSlope, (p.Value-m.QueueDepth.Points[i-1].Value)/seconds)
			}
		}
	}
	saturated := 0
	for _, p := range m.Utilization.Points {
		v.PeakUtilizationPct = math.Max(v.PeakUtilizationPct, p.Value)
		if p.Value >= 90 {
			saturated++
		}
	}
	if len(m.Utilization.Points) > 0 {
		v.UtilizationPct = m.Utilization.ValueAt(plateau)
		v.SaturationFractionPct = 100 * float64(saturated) / float64(len(m.Utilization.Points))
	}
	sustained := v.queuedSamples >= 3 && v.queuedSamples*5 >= v.queueSamples
	v.IsBottleneck = sustained && (v.PeakUtilizationPct >= 80 || v.QueueSlope > 0)
	if v.IsBottleneck {
		v.Classification = "constraint"
		v.Score = 30 + math.Min(25, v.PeakUtilizationPct/4) + math.Min(20, v.SaturationFractionPct/5) + math.Min(15, math.Log10(1+v.MaxQueueDepth)*5) + math.Min(10, math.Log10(1+v.QueueSlope)*4)
	} else if v.queuedSamples > 0 {
		v.Classification = "transient_pressure"
		v.Score = math.Min(20, math.Log10(1+v.MaxQueueDepth)*5) + math.Min(20, v.PeakUtilizationPct/5)
	} else {
		v.Score = math.Min(20, v.PeakUtilizationPct/5)
	}
	return v
}

func applyCausality(v []ResourceVerdict, reachable map[kernel.ResourceID]map[kernel.ResourceID]bool) {
	for up := range v {
		if !v[up].IsBottleneck {
			continue
		}
		for down := range v {
			if up == down || !v[down].IsBottleneck || !reachable[v[up].Resource][v[down].Resource] {
				continue
			}
			if v[down].FirstQueuedTime <= v[up].FirstQueuedTime {
				v[down].Score += 20
				v[down].Classification = "root_constraint"
				v[up].Score -= 15
				v[up].Classification = "upstream_symptom"
			}
		}
	}
}

func finalizeReason(v *ResourceVerdict) {
	if !v.IsBottleneck {
		if v.queuedSamples == 0 {
			v.Reason = fmt.Sprintf("no sustained queue; peak utilization %.1f%%", v.PeakUtilizationPct)
		} else {
			v.Reason = fmt.Sprintf("queueing was transient (%d/%d samples); peak utilization %.1f%%", v.queuedSamples, v.queueSamples, v.PeakUtilizationPct)
		}
		return
	}
	parts := []string{fmt.Sprintf("queue persisted in %d/%d samples", v.queuedSamples, v.queueSamples), fmt.Sprintf("max queue %.0f", v.MaxQueueDepth), fmt.Sprintf("peak utilization %.1f%%", v.PeakUtilizationPct)}
	if v.SaturationFractionPct > 0 {
		parts = append(parts, fmt.Sprintf("saturated for %.1f%% of samples", v.SaturationFractionPct))
	}
	if v.Classification == "root_constraint" {
		parts = append(parts, "downstream pressure appeared no later than its upstream callers")
	}
	if v.Classification == "upstream_symptom" {
		parts = append(parts, "likely propagated backpressure from an earlier downstream constraint")
	}
	v.Reason = strings.Join(parts, "; ")
}

func reachability(graph *ir.Graph) map[kernel.ResourceID]map[kernel.ResourceID]bool {
	result := make(map[kernel.ResourceID]map[kernel.ResourceID]bool)
	if graph == nil {
		return result
	}
	indices := make(map[ir.NodeID]kernel.ResourceID, len(graph.Nodes))
	for i, node := range graph.Nodes {
		indices[node.ID] = kernel.ResourceID(i)
	}
	adjacent := make(map[kernel.ResourceID][]kernel.ResourceID)
	for _, edge := range graph.Edges {
		from, fromOK := indices[edge.From]
		to, toOK := indices[edge.To]
		if fromOK && toOK {
			adjacent[from] = append(adjacent[from], to)
		}
	}
	for start := 0; start < len(graph.Nodes); start++ {
		seen := make(map[kernel.ResourceID]bool)
		stack := append([]kernel.ResourceID(nil), adjacent[kernel.ResourceID(start)]...)
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[current] {
				continue
			}
			seen[current] = true
			stack = append(stack, adjacent[current]...)
		}
		result[kernel.ResourceID(start)] = seen
	}
	return result
}

func detectThroughputDrops(points []metrics.Point) []ThroughputDrop {
	const window = 5
	var drops []ThroughputDrop
	for i := window; i+window <= len(points); i++ {
		before, after := mean(points[i-window:i]), mean(points[i:i+window])
		if before <= 0 || after >= before*.7 {
			continue
		}
		drop := ThroughputDrop{points[i].Time, before, after, 100 * (before - after) / before}
		if len(drops) == 0 || drop.Time-drops[len(drops)-1].Time >= window*kernel.Second {
			drops = append(drops, drop)
		} else if drop.DropPct > drops[len(drops)-1].DropPct {
			drops[len(drops)-1] = drop
		}
	}
	return drops
}

func mean(points []metrics.Point) float64 {
	total := 0.0
	for _, p := range points {
		total += p.Value
	}
	return total / float64(len(points))
}

func detectPlateau(points []metrics.Point) (kernel.SimTime, float64) {
	if len(points) < 6 {
		return 0, 0
	}
	peak := 0.0
	for _, p := range points {
		peak = math.Max(peak, p.Value)
	}
	if peak == 0 {
		return 0, 0
	}
	for i := 2; i+2 < len(points); i++ {
		average := mean(points[i-2 : i+3])
		if average >= peak*.9 {
			return points[i].Time, average
		}
	}
	return 0, 0
}
