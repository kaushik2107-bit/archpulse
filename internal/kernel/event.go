package kernel

type EventType uint8

const (
	RequestArrived EventType = iota
	RoutingDecision
	QueueEnqueued
	ServiceStarted
	ServiceCompleted
	DownstreamCallStarted
	DownstreamCallCompleted
	ResponseSent
	RequestRejected
	RequestTimedOut
	TimeoutCheck
	ResourceDegraded
	ResourceRecovered
	PolicyTick
)

type EventID uint64

type Event struct {
	Time         SimTime
	Seq          uint64
	Type         EventType
	Target       ResourceID
	Payload      any
	CausalParent EventID
	id           EventID
}

func (e Event) ID() EventID { return e.id }

type RequestArrivedPayload struct{ Request *RequestState }
type ServiceCompletedPayload struct{ Request *RequestState }
type DownstreamCallPayload struct {
	Request  *RequestState
	Upstream ResourceID
}
type ResourceDegradedPayload struct {
	LatencyMultiplier float64
	// Instance is one-based. Zero targets the whole logical resource group.
	Instance int
}
