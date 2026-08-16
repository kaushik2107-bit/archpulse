package workload

import (
	"math/rand"

	"archpulse/internal/kernel"
)

type Generator interface {
	NextArrival(after kernel.SimTime, rng *rand.Rand) (kernel.SimTime, bool)
}
