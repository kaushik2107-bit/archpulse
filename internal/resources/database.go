package resources

import "infra-sim/internal/kernel"

type DatabaseResource struct {
	ResourceID  kernel.ResourceID
	ConnPool    *ConnectionPool
	QueryTime   ServiceTimeSampler
	degraded    bool
	latencyMult float64
}

type dbQueryDonePayload struct {
	Request  *kernel.RequestState
	Upstream kernel.ResourceID
}

func (d *DatabaseResource) ID() kernel.ResourceID { return d.ResourceID }
func (d *DatabaseResource) HandleEvent(event kernel.Event, ctx *kernel.SimContext) []kernel.Event {
	switch event.Type {
	case kernel.DownstreamCallStarted:
		payload := event.Payload.(kernel.DownstreamCallPayload)
		if d.ConnPool.Acquire(payload.Request) == Admitted {
			return d.startQuery(event.Time, payload.Request, payload.Upstream, ctx)
		}
	case kernel.ServiceCompleted:
		payload := event.Payload.(dbQueryDonePayload)
		out := []kernel.Event{{Time: event.Time, Type: kernel.DownstreamCallCompleted, Target: payload.Upstream, Payload: kernel.DownstreamCallPayload{Request: payload.Request}}}
		if next := d.ConnPool.Release(); next != nil {
			out = append(out, d.startQuery(event.Time, next, next.Upstream, ctx)...)
		}
		return out
	case kernel.ResourceDegraded:
		d.ApplyFailure(event.Payload.(kernel.ResourceDegradedPayload))
	case kernel.ResourceRecovered:
		d.ClearFailure()
	}
	return nil
}

func (d *DatabaseResource) startQuery(at kernel.SimTime, request *kernel.RequestState, upstream kernel.ResourceID, ctx *kernel.SimContext) []kernel.Event {
	duration := d.QueryTime.Sample(ctx.RNG.ServiceTime)
	if d.degraded {
		duration = kernel.SimTime(float64(duration) * d.latencyMult)
	}
	return []kernel.Event{{Time: at + duration, Type: kernel.ServiceCompleted, Target: d.ResourceID, Payload: dbQueryDonePayload{Request: request, Upstream: upstream}}}
}
func (d *DatabaseResource) SnapshotMetrics() kernel.ResourceMetricsSnapshot {
	scale := max(1, d.ConnPool.MetricScale)
	capacity := d.ConnPool.ReportedCapacity
	if capacity <= 0 {
		capacity = d.ConnPool.MaxConnections
	}
	return kernel.ResourceMetricsSnapshot{ResourceID: d.ResourceID, InFlight: min(capacity, d.ConnPool.InUse*scale), QueueDepth: len(d.ConnPool.Waiters) * scale, StoredQueueDepth: len(d.ConnPool.Waiters), Capacity: capacity, UtilizationPct: d.ConnPool.UtilizationPct()}
}
func (d *DatabaseResource) ApplyFailure(failure kernel.ResourceDegradedPayload) {
	d.degraded = true
	d.latencyMult = failure.LatencyMultiplier
}
func (d *DatabaseResource) ClearFailure() {
	d.degraded = false
	d.latencyMult = 1
}
