package engine

import (
	"testing"

	"infra-sim/internal/ir"
)

func TestEstimateArrivalsHonorsRampAndOverrideHorizon(t *testing.T) {
	workload := ir.WorkloadConfig{Segments: []ir.WorkloadSegment{
		{Type: "constant", Rate: 5_000, StartTimeS: 0, EndTimeS: 60},
		{Type: "ramp", StartRate: 5_000, EndRate: 50_000, StartTimeS: 60, EndTimeS: 180},
		{Type: "constant", Rate: 50_000, StartTimeS: 180, EndTimeS: 300},
	}}
	if got := EstimateArrivals(workload, 0); got != 9_600_000 {
		t.Fatalf("full estimate = %.0f", got)
	}
	if got := EstimateArrivals(workload, 30); got != 150_000 {
		t.Fatalf("30s estimate = %.0f", got)
	}
	if got := RecommendedTrafficScale(9_600_000); got != 48 {
		t.Fatalf("scale = %d", got)
	}
}
