package workload

import (
	"math"
	"math/rand"
	"testing"

	"archpulse/internal/kernel"
)

func TestCompositeCrossesContiguousSegmentBoundary(t *testing.T) {
	generator := NewComposite([]Generator{
		Constant{RatePerSec: 100, StartTime: 0, EndTime: kernel.Second},
		Constant{RatePerSec: 100, StartTime: kernel.Second, EndTime: 2 * kernel.Second},
	})
	rng := rand.New(rand.NewSource(42))
	after := kernel.SimTime(0)
	seenSecond := false
	for {
		next, ok := generator.NextArrival(after, rng)
		if !ok {
			break
		}
		if next >= kernel.Second {
			seenSecond = true
		}
		after = next
	}
	if !seenSecond {
		t.Fatal("composite generator never advanced to second segment")
	}
}

func TestRampEmpiricalRateMatchesLinearTarget(t *testing.T) {
	ramp := Ramp{StartRate: 100, EndRate: 1000, StartTime: 0, EndTime: 60 * kernel.Second}
	rng := rand.New(rand.NewSource(42))
	windowCounts := [3]int{}
	after := kernel.SimTime(0)
	for {
		next, ok := ramp.NextArrival(after, rng)
		if !ok {
			break
		}
		switch {
		case next < 10*kernel.Second:
			windowCounts[0]++
		case next >= 25*kernel.Second && next < 35*kernel.Second:
			windowCounts[1]++
		case next >= 50*kernel.Second:
			windowCounts[2]++
		}
		after = next
	}
	windows := []struct {
		count       int
		midpointSec int
	}{
		{windowCounts[0], 5},
		{windowCounts[1], 30},
		{windowCounts[2], 55},
	}
	for _, window := range windows {
		expected := ramp.RateAt(kernel.SimTime(window.midpointSec)*kernel.Second) * 10
		relativeError := math.Abs(float64(window.count)-expected) / expected
		if relativeError > 0.08 {
			t.Errorf("window at %ds: count=%d expected=%.0f relative error=%.3f", window.midpointSec, window.count, expected, relativeError)
		}
	}
}
