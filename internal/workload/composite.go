package workload

import (
	"math/rand"

	"archpulse/internal/kernel"
)

type Composite struct {
	segments []Generator
	active   int
}

func NewComposite(segments []Generator) *Composite {
	return &Composite{segments: append([]Generator(nil), segments...)}
}

func (c *Composite) NextArrival(after kernel.SimTime, rng *rand.Rand) (kernel.SimTime, bool) {
	for c.active < len(c.segments) {
		if at, ok := c.segments[c.active].NextArrival(after, rng); ok {
			return at, true
		}
		c.active++
	}
	return 0, false
}
