package metrics

import (
	"math"
	"sort"

	"infra-sim/internal/kernel"
)

// Histogram stores logarithmic microsecond buckets, keeping memory bounded.
type Histogram struct {
	buckets map[int]uint64
	count   uint64
	sumUS   float64
}

func NewHistogram() *Histogram { return &Histogram{buckets: make(map[int]uint64)} }

func (h *Histogram) Record(duration kernel.SimTime) {
	h.RecordWeighted(duration, 1)
}

func (h *Histogram) RecordWeighted(duration kernel.SimTime, weight uint64) {
	if weight == 0 {
		weight = 1
	}
	microseconds := math.Max(1, float64(duration)/float64(kernel.Microsecond))
	bucket := int(math.Ceil(math.Log(microseconds) / math.Log(1.01)))
	h.buckets[bucket] += weight
	h.count += weight
	h.sumUS += microseconds * float64(weight)
}

func (h *Histogram) Quantile(percentile float64) float64 {
	if h.count == 0 {
		return 0
	}
	keys := make([]int, 0, len(h.buckets))
	for key := range h.buckets {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	target := uint64(math.Ceil(percentile / 100 * float64(h.count)))
	var seen uint64
	for _, key := range keys {
		seen += h.buckets[key]
		if seen >= target {
			return math.Pow(1.01, float64(key))
		}
	}
	return 0
}

func (h *Histogram) Count() uint64 { return h.count }
func (h *Histogram) Mean() float64 {
	if h.count == 0 {
		return 0
	}
	return h.sumUS / float64(h.count)
}
