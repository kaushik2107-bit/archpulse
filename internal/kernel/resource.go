package kernel

type ResourceID uint32

type SimResource interface {
	ID() ResourceID
	HandleEvent(Event, *SimContext) []Event
	SnapshotMetrics() ResourceMetricsSnapshot
	ApplyFailure(ResourceDegradedPayload)
	ClearFailure()
}

type ResourceMetricsSnapshot struct {
	ResourceID ResourceID
	InFlight   int
	QueueDepth int
	// StoredQueueDepth counts in-memory sampled request objects. QueueDepth may
	// be weighted for reporting and should not be used as a memory guardrail.
	StoredQueueDepth int
	Capacity         int
	UtilizationPct   float64
	Extra            map[string]float64
}

// MetricsSink is owned by the kernel so metrics implementations can depend on
// kernel types without introducing a Go import cycle.
type MetricsSink interface {
	Observe(Event, *World)
	TickInterval() SimTime
}

type SimContext struct {
	Now     SimTime
	RNG     *RNGStreams
	World   *World
	Metrics MetricsSink
}
