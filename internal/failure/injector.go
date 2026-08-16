package failure

import "infra-sim/internal/kernel"

type ScheduledFailure struct {
	At                kernel.SimTime
	Target            kernel.ResourceID
	LatencyMultiplier float64
	Duration          kernel.SimTime
}

func (f ScheduledFailure) Seed(queue *kernel.EventQueue) {
	queue.Push(kernel.Event{Time: f.At, Type: kernel.ResourceDegraded, Target: f.Target, Payload: kernel.ResourceDegradedPayload{LatencyMultiplier: f.LatencyMultiplier}})
	if f.Duration > 0 {
		queue.Push(kernel.Event{Time: f.At + f.Duration, Type: kernel.ResourceRecovered, Target: f.Target})
	}
}
