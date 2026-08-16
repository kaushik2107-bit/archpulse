package ir

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

type yamlSchema struct {
	Services    map[string]yamlService `yaml:"services"`
	Connections []yamlConnection       `yaml:"connections"`
	Workload    yaml.Node              `yaml:"workload"`
	Failures    []yamlFailure          `yaml:"failures"`
}

type yamlService struct {
	Type       string         `yaml:"type"`
	Name       string         `yaml:"name"`
	Parameters map[string]any `yaml:",inline"`
}

type yamlConnection struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

type yamlWorkloadSegment struct {
	Type       string  `yaml:"type"`
	Rate       float64 `yaml:"rate"`
	StartRate  float64 `yaml:"start_rate"`
	EndRate    float64 `yaml:"end_rate"`
	StartTimeS float64 `yaml:"start_time_s"`
	EndTimeS   float64 `yaml:"end_time_s"`
}

type yamlFailure struct {
	Target            string  `yaml:"target"`
	AtS               float64 `yaml:"at_s"`
	LatencyMultiplier float64 `yaml:"latency_multiplier"`
}

func CompileYAML(data []byte) (*Graph, WorkloadConfig, []FailureConfig, error) {
	var raw yamlSchema
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, WorkloadConfig{}, nil, fmt.Errorf("parse yaml: %w", err)
	}

	workloads, err := decodeWorkload(raw.Workload)
	if err != nil {
		return nil, WorkloadConfig{}, nil, err
	}

	names := make([]string, 0, len(raw.Services))
	for name := range raw.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	graph := &Graph{Nodes: make([]Node, 0, len(names)), Edges: make([]Edge, 0, len(raw.Connections))}
	for _, name := range names {
		service := raw.Services[name]
		delete(service.Parameters, "type")
		delete(service.Parameters, "name")
		graph.Nodes = append(graph.Nodes, Node{ID: NodeID(name), Name: service.Name, ResourceType: service.Type, Parameters: service.Parameters})
	}
	for _, connection := range raw.Connections {
		graph.Edges = append(graph.Edges, Edge{From: NodeID(connection.From), To: NodeID(connection.To)})
	}

	failures := make([]FailureConfig, 0, len(raw.Failures))
	for _, failure := range raw.Failures {
		failures = append(failures, FailureConfig{Target: NodeID(failure.Target), AtS: failure.AtS, LatencyMultiplier: failure.LatencyMultiplier})
	}
	config := WorkloadConfig{Segments: workloads}
	if err := Validate(graph, config, failures); err != nil {
		return nil, WorkloadConfig{}, nil, err
	}
	return graph, config, failures, nil
}

func decodeWorkload(node yaml.Node) ([]WorkloadSegment, error) {
	if node.Kind == 0 {
		return nil, fmt.Errorf("workload is required")
	}
	var raw []yamlWorkloadSegment
	if node.Kind == yaml.SequenceNode {
		if err := node.Decode(&raw); err != nil {
			return nil, fmt.Errorf("parse workload: %w", err)
		}
	} else {
		var single yamlWorkloadSegment
		if err := node.Decode(&single); err != nil {
			return nil, fmt.Errorf("parse workload: %w", err)
		}
		raw = []yamlWorkloadSegment{single}
	}
	segments := make([]WorkloadSegment, 0, len(raw))
	for _, segment := range raw {
		segments = append(segments, WorkloadSegment(segment))
	}
	return segments, nil
}
