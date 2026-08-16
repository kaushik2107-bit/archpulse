package ir

type NodeID string

type Node struct {
	ID           NodeID         `json:"id"`
	Name         string         `json:"name,omitempty"`
	ResourceType string         `json:"resource_type"`
	Parameters   map[string]any `json:"parameters"`
}

type Edge struct {
	From NodeID `json:"from"`
	To   NodeID `json:"to"`
}

type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type WorkloadSegment struct {
	Type       string  `json:"type"`
	Rate       float64 `json:"rate,omitempty"`
	StartRate  float64 `json:"start_rate,omitempty"`
	EndRate    float64 `json:"end_rate,omitempty"`
	StartTimeS float64 `json:"start_time_s"`
	EndTimeS   float64 `json:"end_time_s"`
}

type WorkloadConfig struct {
	Segments []WorkloadSegment `json:"segments"`
}

type FailureConfig struct {
	Target            NodeID  `json:"target"`
	AtS               float64 `json:"at_s"`
	LatencyMultiplier float64 `json:"latency_multiplier"`
}
