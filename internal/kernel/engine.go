package kernel

import (
	"errors"
	"fmt"
)

var (
	ErrSimulationCancelled = errors.New("simulation cancelled")
	ErrSimulationLimit     = errors.New("simulation safety limit reached")
)

type ProgressSnapshot struct {
	VirtualTime     SimTime
	Horizon         SimTime
	EventsProcessed uint64
	QueuedRequests  int
	Resources       []ResourceMetricsSnapshot
}

type Engine struct {
	Queue   *EventQueue
	World   *World
	RNG     *RNGStreams
	Metrics MetricsSink
	Horizon SimTime

	// Optional web-run controls. Zero limits preserve the unbounded CLI behavior.
	MaxEvents         uint64
	MaxQueuedRequests int
	ShouldStop        func() bool
	OnProgress        func(ProgressSnapshot)
	now               SimTime
}

type RunTrace struct {
	TotalEventsProcessed uint64
	FinalTime            SimTime
}

func (e *Engine) Run() (RunTrace, error) {
	if err := e.validate(); err != nil {
		return RunTrace{}, err
	}

	var processed uint64
	for e.Queue.Len() > 0 {
		if processed%4096 == 0 {
			if e.ShouldStop != nil && e.ShouldStop() {
				return RunTrace{TotalEventsProcessed: processed, FinalTime: e.now}, ErrSimulationCancelled
			}
			if e.MaxEvents > 0 && processed >= e.MaxEvents {
				return RunTrace{TotalEventsProcessed: processed, FinalTime: e.now}, fmt.Errorf("%w: processed event limit %d", ErrSimulationLimit, e.MaxEvents)
			}
			if e.MaxQueuedRequests > 0 {
				queued := e.queuedRequests()
				if queued > e.MaxQueuedRequests {
					return RunTrace{TotalEventsProcessed: processed, FinalTime: e.now}, fmt.Errorf("%w: queued request limit %d exceeded (%d queued)", ErrSimulationLimit, e.MaxQueuedRequests, queued)
				}
			}
		}
		ev, _ := e.Queue.Pop()
		if ev.Time > e.Horizon {
			break
		}
		e.now = ev.Time
		processed++

		if ev.Type == PolicyTick {
			e.Metrics.Observe(ev, e.World)
			e.publishProgress(processed)
			e.Queue.Push(Event{Time: e.now + e.Metrics.TickInterval(), Type: PolicyTick})
			continue
		}

		resource := e.World.Get(ev.Target)
		if resource == nil {
			return RunTrace{}, fmt.Errorf("event %d targets unknown resource %d", ev.ID(), ev.Target)
		}
		ctx := &SimContext{Now: e.now, RNG: e.RNG, World: e.World, Metrics: e.Metrics}
		followUps := resource.HandleEvent(ev, ctx)
		e.Metrics.Observe(ev, e.World)
		for _, followUp := range followUps {
			followUp.CausalParent = ev.ID()
			if request := requestFromPayload(followUp.Payload); request != nil && followUp.Target != ev.Target {
				request.Upstream = ev.Target
				request.CurrentHop = followUp.Target
			}
			e.Queue.Push(followUp)
		}
	}
	trace := RunTrace{TotalEventsProcessed: processed, FinalTime: e.now}
	e.publishProgress(processed)
	return trace, nil
}

func (e *Engine) queuedRequests() int {
	total := 0
	for index := 0; index < e.World.Len(); index++ {
		snapshot := e.World.Get(ResourceID(index)).SnapshotMetrics()
		if snapshot.StoredQueueDepth > 0 {
			total += snapshot.StoredQueueDepth
		} else {
			total += snapshot.QueueDepth
		}
	}
	return total
}

func (e *Engine) publishProgress(processed uint64) {
	if e.OnProgress == nil {
		return
	}
	resources := make([]ResourceMetricsSnapshot, 0, e.World.Len())
	queued := 0
	for index := 0; index < e.World.Len(); index++ {
		snapshot := e.World.Get(ResourceID(index)).SnapshotMetrics()
		queued += snapshot.QueueDepth
		resources = append(resources, snapshot)
	}
	e.OnProgress(ProgressSnapshot{VirtualTime: e.now, Horizon: e.Horizon, EventsProcessed: processed, QueuedRequests: queued, Resources: resources})
}

func (e *Engine) validate() error {
	if e.Queue == nil || e.World == nil || e.RNG == nil || e.Metrics == nil {
		return fmt.Errorf("engine is not fully initialized")
	}
	if e.Metrics.TickInterval() <= 0 {
		return fmt.Errorf("metrics tick interval must be positive")
	}
	return nil
}

func requestFromPayload(payload any) *RequestState {
	switch value := payload.(type) {
	case RequestArrivedPayload:
		return value.Request
	case ServiceCompletedPayload:
		return value.Request
	case DownstreamCallPayload:
		return value.Request
	default:
		return nil
	}
}
