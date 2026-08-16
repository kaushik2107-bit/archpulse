package ir

type NodeID string

type Node struct {
	ID           NodeID
	ResourceType string
	Parameters   map[string]any
}

type Edge struct {
	From NodeID
	To   NodeID
}

type Graph struct {
	Nodes []Node
	Edges []Edge
}

type WorkloadSegment struct {
	Type       string
	Rate       float64
	StartRate  float64
	EndRate    float64
	StartTimeS float64
	EndTimeS   float64
}

type WorkloadConfig struct{ Segments []WorkloadSegment }

type FailureConfig struct {
	Target            NodeID
	AtS               float64
	LatencyMultiplier float64
}
