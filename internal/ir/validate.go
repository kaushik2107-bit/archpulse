package ir

import "fmt"

func Validate(graph *Graph, workload WorkloadConfig, failures []FailureConfig) error {
	if graph == nil {
		return fmt.Errorf("architecture graph is required")
	}
	ids := make(map[NodeID]Node, len(graph.Nodes))
	loadGenerators := 0
	for _, node := range graph.Nodes {
		if node.ID == "" {
			return fmt.Errorf("node id cannot be empty")
		}
		if _, exists := ids[node.ID]; exists {
			return fmt.Errorf("duplicate node id: %s", node.ID)
		}
		if node.ResourceType == "load_generator" {
			loadGenerators++
		}
		ids[node.ID] = node
	}
	if loadGenerators != 1 {
		return fmt.Errorf("architecture must have exactly one load_generator node")
	}
	for _, edge := range graph.Edges {
		if _, exists := ids[edge.From]; !exists {
			return fmt.Errorf("edge references unknown node: %s", edge.From)
		}
		if _, exists := ids[edge.To]; !exists {
			return fmt.Errorf("edge references unknown node: %s", edge.To)
		}
	}
	if hasCycle(graph) {
		return fmt.Errorf("architecture graph must not contain cycles (MVP limitation)")
	}
	if len(workload.Segments) == 0 {
		return fmt.Errorf("at least one workload segment is required")
	}
	for index, segment := range workload.Segments {
		if segment.Type != "constant" && segment.Type != "ramp" {
			return fmt.Errorf("workload segment %d has unsupported type %q", index, segment.Type)
		}
		if segment.StartTimeS < 0 || segment.EndTimeS <= segment.StartTimeS {
			return fmt.Errorf("workload segment %d has invalid time window", index)
		}
		if segment.Type == "constant" && segment.Rate <= 0 {
			return fmt.Errorf("workload segment %d rate must be positive", index)
		}
		if segment.Type == "ramp" && (segment.StartRate <= 0 || segment.EndRate <= 0) {
			return fmt.Errorf("workload segment %d ramp rates must be positive", index)
		}
		if index > 0 && workload.Segments[index-1].EndTimeS != segment.StartTimeS {
			return fmt.Errorf("workload segments %d and %d must be contiguous", index-1, index)
		}
	}
	for _, failure := range failures {
		node, exists := ids[failure.Target]
		if !exists {
			return fmt.Errorf("failure references unknown node: %s", failure.Target)
		}
		if failure.AtS < 0 || failure.LatencyMultiplier <= 0 || failure.Instance < 0 {
			return fmt.Errorf("failure for %s has invalid timing or multiplier", failure.Target)
		}
		if failure.Instance > 0 {
			instances := numericInt(node.Parameters["instances"])
			if instances == 0 && (node.ResourceType == "aws.ec2" || node.ResourceType == "aws.ecs") {
				instances = 1
			}
			if instances < failure.Instance {
				return fmt.Errorf("failure for %s references instance %d but the service has %d instances", failure.Target, failure.Instance, instances)
			}
		}
	}
	return nil
}

func numericInt(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case uint64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}

func hasCycle(graph *Graph) bool {
	adjacency := make(map[NodeID][]NodeID, len(graph.Nodes))
	for _, edge := range graph.Edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
	}
	state := make(map[NodeID]uint8, len(graph.Nodes))
	var visit func(NodeID) bool
	visit = func(id NodeID) bool {
		if state[id] == 1 {
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, next := range adjacency[id] {
			if visit(next) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for _, node := range graph.Nodes {
		if visit(node.ID) {
			return true
		}
	}
	return false
}
