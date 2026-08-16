package kernel

// SimTime is virtual nanoseconds since the beginning of a simulation.
type SimTime int64

const (
	Nanosecond  SimTime = 1
	Microsecond         = 1000 * Nanosecond
	Millisecond         = 1000 * Microsecond
	Second              = 1000 * Millisecond
)
