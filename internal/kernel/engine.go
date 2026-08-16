package kernel

import "fmt"

type Engine struct {
	Queue   *EventQueue
	World   *World
	RNG     *RNGStreams
	Metrics MetricsSink
	Horizon SimTime
	now     SimTime
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
		ev, _ := e.Queue.Pop()
		if ev.Time > e.Horizon {
			break
		}
		e.now = ev.Time
		processed++

		if ev.Type == PolicyTick {
			e.Metrics.Observe(ev, e.World)
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
	return RunTrace{TotalEventsProcessed: processed, FinalTime: e.now}, nil
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
