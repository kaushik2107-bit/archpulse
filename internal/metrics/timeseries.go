package metrics

import (
	"sort"

	"archpulse/internal/kernel"
)

type Point struct {
	Time  kernel.SimTime `json:"time_ns"`
	Value float64        `json:"value"`
}

type TimeSeries struct {
	Points []Point `json:"points"`
}

func (s *TimeSeries) Record(at kernel.SimTime, value float64) {
	s.Points = append(s.Points, Point{Time: at, Value: value})
}

func (s *TimeSeries) ValueAt(at kernel.SimTime) float64 {
	index := sort.Search(len(s.Points), func(i int) bool { return s.Points[i].Time > at })
	if index == 0 {
		return 0
	}
	return s.Points[index-1].Value
}

func (s *TimeSeries) SlopeOver(start, end kernel.SimTime) float64 {
	if end <= start {
		return 0
	}
	return (s.ValueAt(end) - s.ValueAt(start)) / (float64(end-start) / float64(kernel.Second))
}

type BucketSeries struct {
	Width  kernel.SimTime `json:"width_ns"`
	counts map[int64]uint64
}

func NewBucketSeries(width kernel.SimTime) *BucketSeries {
	return &BucketSeries{Width: width, counts: make(map[int64]uint64)}
}

func (s *BucketSeries) Increment(at kernel.SimTime) { s.IncrementBy(at, 1) }

func (s *BucketSeries) IncrementBy(at kernel.SimTime, count uint64) {
	s.counts[int64(at/s.Width)] += count
}

func (s *BucketSeries) Points() []Point {
	if len(s.counts) == 0 {
		return nil
	}
	keys := make([]int64, 0, len(s.counts))
	for key := range s.counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	points := make([]Point, 0, keys[len(keys)-1]-keys[0]+1)
	for key := keys[0]; key <= keys[len(keys)-1]; key++ {
		points = append(points, Point{Time: kernel.SimTime(key) * s.Width, Value: float64(s.counts[key]) * float64(kernel.Second) / float64(s.Width)})
	}
	return points
}
