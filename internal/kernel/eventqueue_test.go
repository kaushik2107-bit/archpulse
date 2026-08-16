package kernel

import "testing"

func TestEventQueueOrdersByTimeThenPushSequence(t *testing.T) {
	q := NewEventQueue()
	q.Push(Event{Time: 20, Type: ServiceCompleted})
	q.Push(Event{Time: 10, Type: RequestArrived, Target: 1})
	q.Push(Event{Time: 10, Type: RequestArrived, Target: 2})

	wantTargets := []ResourceID{1, 2, 0}
	for i, want := range wantTargets {
		got, ok := q.Pop()
		if !ok {
			t.Fatalf("pop %d: queue unexpectedly empty", i)
		}
		if got.Target != want {
			t.Fatalf("pop %d: target = %d, want %d", i, got.Target, want)
		}
	}
}

func TestRNGStreamsAreDeterministicAndIndependent(t *testing.T) {
	a := NewRNGStreams(42)
	b := NewRNGStreams(42)
	if a.Workload.Int63() != b.Workload.Int63() || a.ServiceTime.Int63() != b.ServiceTime.Int63() {
		t.Fatal("same seed did not reproduce stream values")
	}
	c := NewRNGStreams(42)
	if c.Workload.Int63() == c.ServiceTime.Int63() {
		t.Fatal("named sub-streams unexpectedly produced identical values")
	}
}
