package metrics

import "infra-sim/internal/kernel"

type ResourceMetrics struct {
	Throughput  *BucketSeries
	Latency     *Histogram
	Utilization *TimeSeries
	QueueDepth  *TimeSeries
	Errors      uint64
	Rejected    uint64
}

type InstanceMetrics struct {
	Utilization *TimeSeries
	QueueDepth  *TimeSeries
	Degraded    *TimeSeries
}

type Sink struct {
	perResource map[kernel.ResourceID]*ResourceMetrics
	perInstance map[kernel.ResourceID]map[int]*InstanceMetrics
	global      *ResourceMetrics
	entryPoint  kernel.ResourceID
	tick        kernel.SimTime
}

func NewSink(world *kernel.World, entryPoint kernel.ResourceID, tick kernel.SimTime) *Sink {
	sink := &Sink{perResource: make(map[kernel.ResourceID]*ResourceMetrics, world.Len()), perInstance: make(map[kernel.ResourceID]map[int]*InstanceMetrics), global: newResourceMetrics(), entryPoint: entryPoint, tick: tick}
	for index := 0; index < world.Len(); index++ {
		sink.perResource[kernel.ResourceID(index)] = newResourceMetrics()
	}
	return sink
}

func newResourceMetrics() *ResourceMetrics {
	return &ResourceMetrics{Throughput: NewBucketSeries(kernel.Second), Latency: NewHistogram(), Utilization: &TimeSeries{}, QueueDepth: &TimeSeries{}}
}

func (s *Sink) TickInterval() kernel.SimTime { return s.tick }
func (s *Sink) Observe(event kernel.Event, world *kernel.World) {
	switch event.Type {
	case kernel.ResponseSent:
		payload := event.Payload.(kernel.ServiceCompletedPayload)
		latency := event.Time - payload.Request.ArrivalTime
		weight := requestWeight(payload.Request)
		s.global.Throughput.IncrementBy(event.Time, weight)
		s.global.Latency.RecordWeighted(latency, weight)
		s.perResource[event.Target].Throughput.IncrementBy(event.Time, weight)
		s.perResource[event.Target].Latency.RecordWeighted(latency, weight)
	case kernel.RequestRejected:
		weight := requestWeight(event.Payload.(kernel.RequestArrivedPayload).Request)
		s.global.Rejected += weight
		s.perResource[event.Target].Rejected += weight
	case kernel.PolicyTick:
		for index := 0; index < world.Len(); index++ {
			id := kernel.ResourceID(index)
			snapshot := world.Get(id).SnapshotMetrics()
			s.perResource[id].Utilization.Record(event.Time, snapshot.UtilizationPct)
			s.perResource[id].QueueDepth.Record(event.Time, float64(snapshot.QueueDepth))
			for _, instance := range snapshot.Instances {
				if s.perInstance[id] == nil {
					s.perInstance[id] = make(map[int]*InstanceMetrics)
				}
				series := s.perInstance[id][instance.Instance]
				if series == nil {
					series = &InstanceMetrics{Utilization: &TimeSeries{}, QueueDepth: &TimeSeries{}, Degraded: &TimeSeries{}}
					s.perInstance[id][instance.Instance] = series
				}
				series.Utilization.Record(event.Time, instance.UtilizationPct)
				series.QueueDepth.Record(event.Time, float64(instance.QueueDepth))
				degraded := 0.0
				if instance.Degraded {
					degraded = 1
				}
				series.Degraded.Record(event.Time, degraded)
			}
		}
	}
}

func requestWeight(request *kernel.RequestState) uint64 {
	if request.Weight <= 0 {
		return 1
	}
	return uint64(request.Weight)
}

func (s *Sink) Global() *ResourceMetrics                                    { return s.global }
func (s *Sink) PerResource() map[kernel.ResourceID]*ResourceMetrics         { return s.perResource }
func (s *Sink) PerInstance() map[kernel.ResourceID]map[int]*InstanceMetrics { return s.perInstance }
func (s *Sink) EntryPoint() kernel.ResourceID                               { return s.entryPoint }
