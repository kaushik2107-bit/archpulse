package engine

import (
	"math"

	"archpulse/internal/ir"
)

const TargetRepresentativeRequests = 200_000

// EstimateArrivals integrates offered traffic up to horizonSeconds. A horizon
// of zero uses the complete configured workload.
func EstimateArrivals(workload ir.WorkloadConfig, horizonSeconds float64) float64 {
	total := 0.0
	for _, segment := range workload.Segments {
		end := segment.EndTimeS
		if horizonSeconds > 0 && end > horizonSeconds {
			end = horizonSeconds
		}
		if end <= segment.StartTimeS {
			continue
		}
		duration := end - segment.StartTimeS
		if segment.Type == "ramp" {
			fraction := duration / (segment.EndTimeS - segment.StartTimeS)
			endRate := segment.StartRate + fraction*(segment.EndRate-segment.StartRate)
			total += duration * (segment.StartRate + endRate) / 2
		} else {
			total += duration * segment.Rate
		}
		if horizonSeconds > 0 && end >= horizonSeconds {
			break
		}
	}
	return total
}

func RecommendedTrafficScale(estimatedArrivals float64) int {
	return max(1, int(math.Ceil(estimatedArrivals/TargetRepresentativeRequests)))
}
