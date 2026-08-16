package kernel

import "testing"

type testMetrics struct {
	ticks int
}

func (m *testMetrics) Observe(event Event, _ *World) {
	if event.Type == PolicyTick {
		m.ticks++
	}
}
func (m *testMetrics) TickInterval() SimTime { return Second }

type forwardingResource struct {
	id         ResourceID
	downstream ResourceID
	handled    int
}

func (r *forwardingResource) ID() ResourceID { return r.id }
func (r *forwardingResource) HandleEvent(event Event, _ *SimContext) []Event {
	r.handled++
	request := event.Payload.(RequestArrivedPayload).Request
	return []Event{{Time: event.Time, Type: RequestArrived, Target: r.downstream, Payload: RequestArrivedPayload{Request: request}}}
}
func (r *forwardingResource) SnapshotMetrics() ResourceMetricsSnapshot {
	return ResourceMetricsSnapshot{ResourceID: r.id}
}
func (r *forwardingResource) ApplyFailure(ResourceDegradedPayload) {}
func (r *forwardingResource) ClearFailure()                        {}

type recordingResource struct {
	id      ResourceID
	handled int
	request *RequestState
}

func (r *recordingResource) ID() ResourceID { return r.id }
func (r *recordingResource) HandleEvent(event Event, _ *SimContext) []Event {
	r.handled++
	r.request = event.Payload.(RequestArrivedPayload).Request
	return nil
}
func (r *recordingResource) SnapshotMetrics() ResourceMetricsSnapshot {
	return ResourceMetricsSnapshot{ResourceID: r.id}
}
func (r *recordingResource) ApplyFailure(ResourceDegradedPayload) {}
func (r *recordingResource) ClearFailure()                        {}

func TestEngineHandlesPolicyTickAndRequestHopBookkeeping(t *testing.T) {
	world := &World{}
	firstID := world.Reserve()
	secondID := world.Reserve()
	first := &forwardingResource{id: firstID, downstream: secondID}
	second := &recordingResource{id: secondID}
	if err := world.Set(firstID, first); err != nil {
		t.Fatal(err)
	}
	if err := world.Set(secondID, second); err != nil {
		t.Fatal(err)
	}
	queue := NewEventQueue()
	request := &RequestState{ID: 1}
	queue.Push(Event{Time: 0, Type: RequestArrived, Target: firstID, Payload: RequestArrivedPayload{Request: request}})
	queue.Push(Event{Time: 0, Type: PolicyTick})
	metricSink := &testMetrics{}
	engine := &Engine{Queue: queue, World: world, RNG: NewRNGStreams(1), Metrics: metricSink, Horizon: 0}
	if _, err := engine.Run(); err != nil {
		t.Fatal(err)
	}
	if first.handled != 1 || second.handled != 1 {
		t.Fatalf("unexpected dispatch counts: first=%d second=%d", first.handled, second.handled)
	}
	if metricSink.ticks != 1 {
		t.Fatalf("policy ticks observed = %d, want 1", metricSink.ticks)
	}
	if second.request.Upstream != firstID || second.request.CurrentHop != secondID {
		t.Fatalf("hop bookkeeping = upstream %d current %d, want %d and %d", second.request.Upstream, second.request.CurrentHop, firstID, secondID)
	}
}
