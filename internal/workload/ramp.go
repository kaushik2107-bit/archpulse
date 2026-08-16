package workload

import (
	"math"
	"math/rand"

	"infra-sim/internal/kernel"
)

type Ramp struct {
	StartRate float64
	EndRate   float64
	StartTime kernel.SimTime
	EndTime   kernel.SimTime
}

func (r Ramp) RateAt(at kernel.SimTime) float64 {
	if at <= r.StartTime {
		return r.StartRate
	}
	if at >= r.EndTime {
		return r.EndRate
	}
	fraction := float64(at-r.StartTime) / float64(r.EndTime-r.StartTime)
	return r.StartRate + fraction*(r.EndRate-r.StartRate)
}

func (r Ramp) NextArrival(after kernel.SimTime, rng *rand.Rand) (kernel.SimTime, bool) {
	maxRate := math.Max(r.StartRate, r.EndRate)
	if maxRate <= 0 || r.EndTime <= r.StartTime || after >= r.EndTime {
		return 0, false
	}
	t := after
	if t < r.StartTime {
		t = r.StartTime
	}
	for {
		t += kernel.SimTime(-math.Log1p(-rng.Float64()) / (maxRate / 1e9))
		if t >= r.EndTime {
			return 0, false
		}
		if rng.Float64() < r.RateAt(t)/maxRate {
			return t, true
		}
	}
}
