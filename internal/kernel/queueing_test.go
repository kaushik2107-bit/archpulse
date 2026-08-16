package kernel_test

import (
	"math"
	"math/rand"
	"testing"

	"archpulse/internal/kernel"
)

type mm1Resource struct {
	id      kernel.ResourceID
	mu      float64
	busy    bool
	waiting []*kernel.RequestState
}

func (r *mm1Resource) ID() kernel.ResourceID { return r.id }
func (r *mm1Resource) HandleEvent(event kernel.Event, ctx *kernel.SimContext) []kernel.Event {
	switch event.Type {
	case kernel.RequestArrived:
		request := event.Payload.(kernel.RequestArrivedPayload).Request
		if r.busy {
			r.waiting = append(r.waiting, request)
			return nil
		}
		r.busy = true
		return r.completeAfter(event.Time, request, ctx.RNG.ServiceTime)
	case kernel.ServiceCompleted:
		request := event.Payload.(kernel.ServiceCompletedPayload).Request
		out := []kernel.Event{{Time: event.Time, Type: kernel.ResponseSent, Target: r.id, Payload: kernel.ServiceCompletedPayload{Request: request}}}
		if len(r.waiting) == 0 {
			r.busy = false
			return out
		}
		next := r.waiting[0]
		r.waiting = r.waiting[1:]
		return append(out, r.completeAfter(event.Time, next, ctx.RNG.ServiceTime)...)
	}
	return nil
}
func (r *mm1Resource) completeAfter(at kernel.SimTime, request *kernel.RequestState, rng *rand.Rand) []kernel.Event {
	duration := kernel.SimTime(-math.Log1p(-rng.Float64()) / (r.mu / 1e9))
	return []kernel.Event{{Time: at + duration, Type: kernel.ServiceCompleted, Target: r.id, Payload: kernel.ServiceCompletedPayload{Request: request}}}
}
func (r *mm1Resource) SnapshotMetrics() kernel.ResourceMetricsSnapshot {
	inFlight := 0
	if r.busy {
		inFlight = 1
	}
	return kernel.ResourceMetricsSnapshot{ResourceID: r.id, Capacity: 1, InFlight: inFlight, QueueDepth: len(r.waiting)}
}
func (r *mm1Resource) ApplyFailure(kernel.ResourceDegradedPayload) {}
func (r *mm1Resource) ClearFailure()                               {}

type latencySink struct {
	startMeasuring kernel.SimTime
	total          kernel.SimTime
	count          uint64
}

func (s *latencySink) Observe(event kernel.Event, _ *kernel.World) {
	if event.Type != kernel.ResponseSent {
		return
	}
	request := event.Payload.(kernel.ServiceCompletedPayload).Request
	if request.ArrivalTime >= s.startMeasuring {
		s.total += event.Time - request.ArrivalTime
		s.count++
	}
}
func (s *latencySink) TickInterval() kernel.SimTime { return kernel.Second }

func TestMM1MeanResponseTimeMatchesClosedForm(t *testing.T) {
	const lambda = 80.0
	const mu = 100.0
	const seed = int64(42)
	world := &kernel.World{}
	id := world.Reserve()
	if err := world.Set(id, &mm1Resource{id: id, mu: mu}); err != nil {
		t.Fatal(err)
	}
	queue := kernel.NewEventQueue()
	rng := kernel.NewRNGStreams(seed).Workload
	arrival := kernel.SimTime(0)
	arrivalEnd := 540 * kernel.Second
	var requestID kernel.RequestID
	for arrival < arrivalEnd {
		arrival += kernel.SimTime(-math.Log1p(-rng.Float64()) / (lambda / 1e9))
		if arrival >= arrivalEnd {
			break
		}
		requestID++
		request := &kernel.RequestState{ID: requestID, ArrivalTime: arrival}
		queue.Push(kernel.Event{Time: arrival, Type: kernel.RequestArrived, Target: id, Payload: kernel.RequestArrivedPayload{Request: request}})
	}
	sink := &latencySink{startMeasuring: 60 * kernel.Second}
	engine := &kernel.Engine{Queue: queue, World: world, RNG: kernel.NewRNGStreams(seed), Metrics: sink, Horizon: 600 * kernel.Second}
	if _, err := engine.Run(); err != nil {
		t.Fatal(err)
	}
	observedMs := float64(sink.total) / float64(sink.count) / float64(kernel.Millisecond)
	expectedMs := 1000 / (mu - lambda)
	relativeError := math.Abs(observedMs-expectedMs) / expectedMs
	if relativeError > 0.08 {
		t.Fatalf("mean response = %.2fms, want %.2fms within 8%% (error %.2f%%)", observedMs, expectedMs, 100*relativeError)
	}
}
