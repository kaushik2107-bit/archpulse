package resources

import (
	"archpulse/internal/kernel"
	"archpulse/internal/workload"
)

type LoadGenerator struct {
	ResourceID    kernel.ResourceID
	Downstream    kernel.ResourceID
	Workload      workload.Generator
	RequestWeight int
	nextReqID     kernel.RequestID
}

func (g *LoadGenerator) ID() kernel.ResourceID { return g.ResourceID }
func (g *LoadGenerator) HandleEvent(event kernel.Event, ctx *kernel.SimContext) []kernel.Event {
	if event.Type != kernel.RequestArrived || g.Workload == nil {
		return nil
	}
	nextTime, ok := g.Workload.NextArrival(event.Time, ctx.RNG.Workload)
	if !ok {
		return nil
	}
	g.nextReqID++
	weight := g.RequestWeight
	if weight <= 0 {
		weight = 1
	}
	request := &kernel.RequestState{ID: g.nextReqID, ArrivalTime: nextTime, Weight: weight}
	return []kernel.Event{
		{Time: nextTime, Type: kernel.RequestArrived, Target: g.Downstream, Payload: kernel.RequestArrivedPayload{Request: request}},
		{Time: nextTime, Type: kernel.RequestArrived, Target: g.ResourceID},
	}
}
func (g *LoadGenerator) SnapshotMetrics() kernel.ResourceMetricsSnapshot {
	return kernel.ResourceMetricsSnapshot{ResourceID: g.ResourceID}
}
func (g *LoadGenerator) ApplyFailure(kernel.ResourceDegradedPayload) {}
func (g *LoadGenerator) ClearFailure()                               {}
