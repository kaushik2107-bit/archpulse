package resources

import "archpulse/internal/kernel"

type LoadBalancer struct {
	ResourceID kernel.ResourceID
	Backends   []kernel.ResourceID
	rrIndex    int
}

func (lb *LoadBalancer) ID() kernel.ResourceID { return lb.ResourceID }
func (lb *LoadBalancer) HandleEvent(event kernel.Event, _ *kernel.SimContext) []kernel.Event {
	if event.Type != kernel.RequestArrived || len(lb.Backends) == 0 {
		return nil
	}
	payload := event.Payload.(kernel.RequestArrivedPayload)
	backend := lb.Backends[lb.rrIndex%len(lb.Backends)]
	lb.rrIndex++
	return []kernel.Event{{Time: event.Time, Type: kernel.RequestArrived, Target: backend, Payload: payload}}
}
func (lb *LoadBalancer) SnapshotMetrics() kernel.ResourceMetricsSnapshot {
	return kernel.ResourceMetricsSnapshot{ResourceID: lb.ResourceID}
}
func (lb *LoadBalancer) ApplyFailure(kernel.ResourceDegradedPayload) {}
func (lb *LoadBalancer) ClearFailure()                               {}
