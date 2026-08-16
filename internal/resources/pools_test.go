package resources

import (
	"testing"

	"infra-sim/internal/kernel"
)

func TestServerPoolQueuesAndResumesFIFO(t *testing.T) {
	pool := &ServerPool{Capacity: 1}
	first := &kernel.RequestState{ID: 1}
	second := &kernel.RequestState{ID: 2}
	if got := pool.TryAdmit(first); got != Admitted {
		t.Fatalf("first result = %v, want admitted", got)
	}
	if got := pool.TryAdmit(second); got != Queued {
		t.Fatalf("second result = %v, want queued", got)
	}
	if got := pool.OnServiceComplete(); got != second {
		t.Fatalf("resumed request = %v, want second request", got)
	}
	if pool.InFlight != 1 || len(pool.Queue) != 0 {
		t.Fatalf("unexpected pool state: in-flight=%d queue=%d", pool.InFlight, len(pool.Queue))
	}
}
