package resources

import (
	"math"
	"math/rand"

	"archpulse/internal/kernel"
)

type ServiceTimeSampler interface {
	Sample(*rand.Rand) kernel.SimTime
}

type LognormalSampler struct {
	MeanMs   float64
	StdDevMs float64
}

func (s LognormalSampler) Sample(rng *rand.Rand) kernel.SimTime {
	if s.MeanMs <= 0 {
		return 0
	}
	if s.StdDevMs <= 0 {
		return kernel.SimTime(s.MeanMs * float64(kernel.Millisecond))
	}
	variance := s.StdDevMs * s.StdDevMs
	sigmaSquared := math.Log1p(variance / (s.MeanMs * s.MeanMs))
	mu := math.Log(s.MeanMs) - sigmaSquared/2
	ms := math.Exp(mu + math.Sqrt(sigmaSquared)*rng.NormFloat64())
	return kernel.SimTime(ms * float64(kernel.Millisecond))
}
