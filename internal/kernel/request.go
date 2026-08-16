package kernel

type RequestID uint64

type RequestState struct {
	ID            RequestID
	ArrivalTime   SimTime
	CurrentHop    ResourceID
	Upstream      ResourceID
	PathHistory   []HopRecord
	OperationType string
	SizeBytes     int64
	Deadline      SimTime
	RetriesSoFar  int
	// Weight is the number of real requests represented by this sampled request.
	// Exact simulations use 1.
	Weight int
}

type HopRecord struct {
	Resource ResourceID
	EnterAt  SimTime
	ExitAt   SimTime
}
