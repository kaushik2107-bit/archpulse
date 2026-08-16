package resources

import "infra-sim/internal/kernel"

type ComputeResource struct {
	ResourceID kernel.ResourceID
	Pool       *ServerPool
	Downstream kernel.ResourceID
}

func (c *ComputeResource) ID() kernel.ResourceID { return c.ResourceID }
func (c *ComputeResource) HandleEvent(event kernel.Event, ctx *kernel.SimContext) []kernel.Event {
	switch event.Type {
	case kernel.RequestArrived:
		request := event.Payload.(kernel.RequestArrivedPayload).Request
		switch c.Pool.TryAdmit(request) {
		case Rejected:
			return []kernel.Event{{Time: event.Time, Type: kernel.RequestRejected, Target: c.ResourceID, Payload: kernel.RequestArrivedPayload{Request: request}}}
		case Queued:
			return nil
		default:
			return c.startDownstreamCall(event.Time, request)
		}
	case kernel.DownstreamCallCompleted:
		request := event.Payload.(kernel.DownstreamCallPayload).Request
		serviceTime := c.Pool.ServiceTime.Sample(ctx.RNG.ServiceTime)
		return []kernel.Event{{Time: event.Time + serviceTime, Type: kernel.ServiceCompleted, Target: c.ResourceID, Payload: kernel.ServiceCompletedPayload{Request: request}}}
	case kernel.ServiceCompleted:
		request := event.Payload.(kernel.ServiceCompletedPayload).Request
		out := []kernel.Event{{Time: event.Time, Type: kernel.ResponseSent, Target: c.ResourceID, Payload: kernel.ServiceCompletedPayload{Request: request}}}
		if next := c.Pool.OnServiceComplete(); next != nil {
			out = append(out, c.startDownstreamCall(event.Time, next)...)
		}
		return out
	}
	return nil
}

func (c *ComputeResource) startDownstreamCall(at kernel.SimTime, request *kernel.RequestState) []kernel.Event {
	return []kernel.Event{{Time: at, Type: kernel.DownstreamCallStarted, Target: c.Downstream, Payload: kernel.DownstreamCallPayload{Request: request, Upstream: c.ResourceID}}}
}
func (c *ComputeResource) SnapshotMetrics() kernel.ResourceMetricsSnapshot {
	utilization := 0.0
	if c.Pool.Capacity > 0 {
		utilization = 100 * float64(c.Pool.InFlight) / float64(c.Pool.Capacity)
	}
	scale := max(1, c.Pool.MetricScale)
	capacity := c.Pool.ReportedCapacity
	if capacity <= 0 {
		capacity = c.Pool.Capacity
	}
	return kernel.ResourceMetricsSnapshot{ResourceID: c.ResourceID, InFlight: min(capacity, c.Pool.InFlight*scale), QueueDepth: len(c.Pool.Queue) * scale, StoredQueueDepth: len(c.Pool.Queue), Capacity: capacity, UtilizationPct: utilization}
}
func (c *ComputeResource) ApplyFailure(kernel.ResourceDegradedPayload) {}
func (c *ComputeResource) ClearFailure()                               {}
