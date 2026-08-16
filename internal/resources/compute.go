package resources

import "infra-sim/internal/kernel"

type ComputeInstance struct {
	Pool              *ServerPool
	Degraded          bool
	LatencyMultiplier float64
}

// ComputeResource is a logical service group. Requests enter through the graph
// node and are assigned to individually scheduled replicas.
type ComputeResource struct {
	ResourceID kernel.ResourceID
	Instances  []*ComputeInstance
	Downstream kernel.ResourceID
	next       int
	assigned   map[kernel.RequestID]int
}

func (c *ComputeResource) ID() kernel.ResourceID { return c.ResourceID }

func (c *ComputeResource) HandleEvent(event kernel.Event, ctx *kernel.SimContext) []kernel.Event {
	switch event.Type {
	case kernel.RequestArrived:
		request := event.Payload.(kernel.RequestArrivedPayload).Request
		instance := c.chooseInstance()
		if instance < 0 {
			return []kernel.Event{{Time: event.Time, Type: kernel.RequestRejected, Target: c.ResourceID, Payload: kernel.RequestArrivedPayload{Request: request}}}
		}
		if c.assigned == nil {
			c.assigned = make(map[kernel.RequestID]int)
		}
		c.assigned[request.ID] = instance
		switch c.Instances[instance].Pool.TryAdmit(request) {
		case Rejected:
			delete(c.assigned, request.ID)
			return []kernel.Event{{Time: event.Time, Type: kernel.RequestRejected, Target: c.ResourceID, Payload: kernel.RequestArrivedPayload{Request: request}}}
		case Queued:
			return nil
		default:
			return c.startDownstreamCall(event.Time, request)
		}
	case kernel.DownstreamCallCompleted:
		request := event.Payload.(kernel.DownstreamCallPayload).Request
		instance, exists := c.assigned[request.ID]
		if !exists {
			return nil
		}
		member := c.Instances[instance]
		serviceTime := member.Pool.ServiceTime.Sample(ctx.RNG.ServiceTime)
		if member.Degraded {
			serviceTime = kernel.SimTime(float64(serviceTime) * member.LatencyMultiplier)
		}
		return []kernel.Event{{Time: event.Time + serviceTime, Type: kernel.ServiceCompleted, Target: c.ResourceID, Payload: kernel.ServiceCompletedPayload{Request: request}}}
	case kernel.ServiceCompleted:
		request := event.Payload.(kernel.ServiceCompletedPayload).Request
		instance, exists := c.assigned[request.ID]
		if !exists {
			return nil
		}
		delete(c.assigned, request.ID)
		out := []kernel.Event{{Time: event.Time, Type: kernel.ResponseSent, Target: c.ResourceID, Payload: kernel.ServiceCompletedPayload{Request: request}}}
		if next := c.Instances[instance].Pool.OnServiceComplete(); next != nil {
			out = append(out, c.startDownstreamCall(event.Time, next)...)
		}
		return out
	case kernel.ResourceDegraded:
		c.ApplyFailure(event.Payload.(kernel.ResourceDegradedPayload))
	case kernel.ResourceRecovered:
		c.ClearFailure()
	}
	return nil
}

func (c *ComputeResource) chooseInstance() int {
	if len(c.Instances) == 0 {
		return -1
	}
	best := -1
	bestLoad := 0.0
	for offset := range c.Instances {
		index := (c.next + offset) % len(c.Instances)
		pool := c.Instances[index].Pool
		load := float64(pool.InFlight+len(pool.Queue)) / float64(max(1, pool.Capacity))
		if best < 0 || load < bestLoad {
			best, bestLoad = index, load
		}
	}
	c.next = (best + 1) % len(c.Instances)
	return best
}

func (c *ComputeResource) startDownstreamCall(at kernel.SimTime, request *kernel.RequestState) []kernel.Event {
	return []kernel.Event{{Time: at, Type: kernel.DownstreamCallStarted, Target: c.Downstream, Payload: kernel.DownstreamCallPayload{Request: request, Upstream: c.ResourceID}}}
}

func (c *ComputeResource) SnapshotMetrics() kernel.ResourceMetricsSnapshot {
	snapshot := kernel.ResourceMetricsSnapshot{ResourceID: c.ResourceID, Instances: make([]kernel.InstanceMetricsSnapshot, 0, len(c.Instances))}
	for index, member := range c.Instances {
		pool := member.Pool
		scale := max(1, pool.MetricScale)
		capacity := pool.ReportedCapacity
		if capacity <= 0 {
			capacity = pool.Capacity
		}
		inFlight := min(capacity, pool.InFlight*scale)
		queue := len(pool.Queue) * scale
		utilization := 100 * float64(pool.InFlight) / float64(max(1, pool.Capacity))
		snapshot.InFlight += inFlight
		snapshot.QueueDepth += queue
		snapshot.StoredQueueDepth += len(pool.Queue)
		snapshot.Capacity += capacity
		snapshot.Instances = append(snapshot.Instances, kernel.InstanceMetricsSnapshot{Instance: index + 1, InFlight: inFlight, QueueDepth: queue, Capacity: capacity, UtilizationPct: utilization, Degraded: member.Degraded})
	}
	if snapshot.Capacity > 0 {
		snapshot.UtilizationPct = 100 * float64(snapshot.InFlight) / float64(snapshot.Capacity)
	}
	return snapshot
}

func (c *ComputeResource) ApplyFailure(failure kernel.ResourceDegradedPayload) {
	for index, member := range c.Instances {
		if failure.Instance == 0 || failure.Instance == index+1 {
			member.Degraded = true
			member.LatencyMultiplier = failure.LatencyMultiplier
		}
	}
}

func (c *ComputeResource) ClearFailure() {
	for _, member := range c.Instances {
		member.Degraded = false
		member.LatencyMultiplier = 1
	}
}
