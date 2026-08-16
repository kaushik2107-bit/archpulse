package metrics

import (
	"testing"

	"infra-sim/internal/kernel"
)

func TestWeightedMetricsRepresentSampledRequests(t *testing.T) {
	histogram := NewHistogram()
	histogram.RecordWeighted(10*kernel.Millisecond, 20)
	if histogram.Count() != 20 {
		t.Fatalf("histogram count = %d, want 20", histogram.Count())
	}
	if histogram.Mean() != 10_000 {
		t.Fatalf("histogram mean = %f us, want 10000", histogram.Mean())
	}
	series := NewBucketSeries(kernel.Second)
	series.IncrementBy(500*kernel.Millisecond, 20)
	points := series.Points()
	if len(points) != 1 || points[0].Value != 20 {
		t.Fatalf("weighted throughput = %+v, want 20 RPS", points)
	}
}
