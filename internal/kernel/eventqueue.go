package kernel

import "container/heap"

type EventQueue struct {
	heap    eventHeap
	nextSeq uint64
	nextID  EventID
}

func NewEventQueue() *EventQueue {
	q := &EventQueue{}
	heap.Init(&q.heap)
	return q
}

func (q *EventQueue) Push(e Event) EventID {
	e.Seq = q.nextSeq
	q.nextSeq++
	q.nextID++
	e.id = q.nextID
	heap.Push(&q.heap, e)
	return e.id
}

func (q *EventQueue) Pop() (Event, bool) {
	if q.heap.Len() == 0 {
		return Event{}, false
	}
	return heap.Pop(&q.heap).(Event), true
}

func (q *EventQueue) Len() int { return q.heap.Len() }

type eventHeap []Event

func (h eventHeap) Len() int      { return len(h) }
func (h eventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h eventHeap) Less(i, j int) bool {
	if h[i].Time != h[j].Time {
		return h[i].Time < h[j].Time
	}
	return h[i].Seq < h[j].Seq
}
func (h *eventHeap) Push(x any) { *h = append(*h, x.(Event)) }
func (h *eventHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
