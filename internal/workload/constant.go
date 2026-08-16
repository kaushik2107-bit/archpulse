package workload

import (
	"math"
	"math/rand"

	"infra-sim/internal/kernel"
)

type Constant struct {
	RatePerSec float64
	StartTime  kernel.SimTime
	EndTime    kernel.SimTime
}

func (c Constant) NextArrival(after kernel.SimTime, rng *rand.Rand) (kernel.SimTime, bool) {
	if c.RatePerSec <= 0 || c.EndTime <= c.StartTime || after >= c.EndTime {
		return 0, false
	}
	base := after
	if base < c.StartTime {
		base = c.StartTime
	}
	u := rng.Float64()
	interArrival := -math.Log1p(-u) / (c.RatePerSec / 1e9)
	next := base + kernel.SimTime(interArrival)
	return next, next < c.EndTime
}
