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
}

type HopRecord struct {
	Resource ResourceID
	EnterAt  SimTime
	ExitAt   SimTime
}
