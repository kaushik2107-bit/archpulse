package analysis

import (
	"sort"

	"infra-sim/internal/kernel"
	"infra-sim/internal/metrics"
)

type BottleneckReport struct {
	PlateauStartTime  kernel.SimTime    `json:"plateau_start_time_ns"`
	PlateauThroughput float64           `json:"plateau_throughput_rps"`
	Ranked            []ResourceVerdict `json:"ranked_resources"`
}

type ResourceVerdict struct {
	Resource       kernel.ResourceID `json:"resource_id"`
	UtilizationPct float64           `json:"utilization_pct"`
	QueueDepth     float64           `json:"queue_depth"`
	QueueSlope     float64           `json:"queue_slope_per_sec"`
	IsBottleneck   bool              `json:"is_bottleneck"`
	Reason         string            `json:"reason"`
}

func Analyze(_ kernel.RunTrace, sink *metrics.Sink) BottleneckReport {
	throughput := sink.Global().Throughput.Points()
	plateauTime, plateauThroughput := detectPlateau(throughput)
	ids := make([]int, 0, len(sink.PerResource()))
	for id := range sink.PerResource() {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	verdicts := make([]ResourceVerdict, 0, len(ids))
	for _, rawID := range ids {
		id := kernel.ResourceID(rawID)
		resourceMetrics := sink.PerResource()[id]
		evaluationTime := plateauTime
		if evaluationTime == 0 && len(resourceMetrics.QueueDepth.Points) > 0 {
			evaluationTime = resourceMetrics.QueueDepth.Points[len(resourceMetrics.QueueDepth.Points)-1].Time
		}
		end := evaluationTime + 10*kernel.Second
		if points := resourceMetrics.QueueDepth.Points; len(points) > 0 && end > points[len(points)-1].Time {
			end = points[len(points)-1].Time
		}
		queue := resourceMetrics.QueueDepth.ValueAt(evaluationTime)
		slope := resourceMetrics.QueueDepth.SlopeOver(evaluationTime, end)
		utilization := resourceMetrics.Utilization.ValueAt(evaluationTime)
		verdict := ResourceVerdict{Resource: id, UtilizationPct: utilization, QueueDepth: queue, QueueSlope: slope}
		if queue > 0 && slope > 0 {
			verdict.IsBottleneck = true
			verdict.Reason = "queue depth is positive and growing at the throughput plateau"
		} else {
			verdict.Reason = "no sustained queue growth observed at the throughput plateau"
		}
		verdicts = append(verdicts, verdict)
	}
	sort.SliceStable(verdicts, func(i, j int) bool {
		if verdicts[i].IsBottleneck != verdicts[j].IsBottleneck {
			return verdicts[i].IsBottleneck
		}
		if verdicts[i].QueueSlope != verdicts[j].QueueSlope {
			return verdicts[i].QueueSlope > verdicts[j].QueueSlope
		}
		if verdicts[i].UtilizationPct != verdicts[j].UtilizationPct {
			return verdicts[i].UtilizationPct > verdicts[j].UtilizationPct
		}
		return verdicts[i].Resource < verdicts[j].Resource
	})
	return BottleneckReport{PlateauStartTime: plateauTime, PlateauThroughput: plateauThroughput, Ranked: verdicts}
}

func detectPlateau(points []metrics.Point) (kernel.SimTime, float64) {
	if len(points) < 4 {
		return 0, 0
	}
	baselineGrowth := points[1].Value - points[0].Value
	if baselineGrowth <= 0 {
		for index := 2; index < len(points); index++ {
			if growth := points[index].Value - points[index-1].Value; growth > baselineGrowth {
				baselineGrowth = growth
			}
		}
	}
	if baselineGrowth <= 0 {
		return 0, 0
	}
	for index := 2; index < len(points); index++ {
		growth := points[index].Value - points[index-1].Value
		if growth < 0.05*baselineGrowth {
			return points[index].Time, points[index].Value
		}
	}
	return 0, 0
}
