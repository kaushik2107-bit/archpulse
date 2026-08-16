package kernel

import (
	"hash/fnv"
	"math/rand"
)

type RNGStreams struct {
	Workload    *rand.Rand
	ServiceTime *rand.Rand
	Failure     *rand.Rand
}

func NewRNGStreams(masterSeed int64) *RNGStreams {
	return &RNGStreams{
		Workload:    rand.New(rand.NewSource(deriveSeed(masterSeed, "workload"))),
		ServiceTime: rand.New(rand.NewSource(deriveSeed(masterSeed, "service-time"))),
		Failure:     rand.New(rand.NewSource(deriveSeed(masterSeed, "failure"))),
	}
}

func deriveSeed(master int64, stream string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(stream))
	return int64(h.Sum64() ^ uint64(master))
}
