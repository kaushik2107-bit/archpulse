package resources

import (
	"fmt"
	"sort"

	"infra-sim/internal/ir"
	"infra-sim/internal/kernel"
	"infra-sim/internal/profiles"
	"infra-sim/internal/workload"
)

type ResourceDeps struct{ Downstream []kernel.ResourceID }

func BuildWorld(graph *ir.Graph, workloadConfig ir.WorkloadConfig) (*kernel.World, map[ir.NodeID]kernel.ResourceID, error) {
	world := &kernel.World{}
	ids := make(map[ir.NodeID]kernel.ResourceID, len(graph.Nodes))
	for _, node := range graph.Nodes {
		ids[node.ID] = world.Reserve()
	}
	generator, err := buildWorkload(workloadConfig)
	if err != nil {
		return nil, nil, err
	}
	for _, node := range graph.Nodes {
		simClass, parameters, err := profiles.Resolve(node.ResourceType, node.Parameters)
		if err != nil {
			return nil, nil, fmt.Errorf("node %s: %w", node.ID, err)
		}
		downstream := downstreamIDs(graph, node.ID, ids)
		resource, err := construct(simClass, ids[node.ID], parameters, downstream, generator)
		if err != nil {
			return nil, nil, fmt.Errorf("node %s: %w", node.ID, err)
		}
		if err := world.Set(ids[node.ID], resource); err != nil {
			return nil, nil, err
		}
	}
	return world, ids, nil
}

func construct(simClass string, id kernel.ResourceID, parameters map[string]any, downstream []kernel.ResourceID, generator workload.Generator) (kernel.SimResource, error) {
	switch simClass {
	case "load_generator":
		if len(downstream) != 1 {
			return nil, fmt.Errorf("load generator requires exactly one downstream resource")
		}
		return &LoadGenerator{ResourceID: id, Downstream: downstream[0], Workload: generator}, nil
	case "load_balancer":
		if len(downstream) == 0 {
			return nil, fmt.Errorf("load balancer requires at least one backend")
		}
		return &LoadBalancer{ResourceID: id, Backends: downstream}, nil
	case "compute":
		if len(downstream) != 1 {
			return nil, fmt.Errorf("compute requires exactly one downstream resource")
		}
		instances, err := intParameter(parameters, "instances")
		if err != nil {
			return nil, err
		}
		workers, err := intParameter(parameters, "workers_per_instance")
		if err != nil {
			return nil, err
		}
		mean, err := floatParameter(parameters, "service_time_mean_ms")
		if err != nil {
			return nil, err
		}
		stddev, err := floatParameter(parameters, "service_time_stddev_ms")
		if err != nil {
			return nil, err
		}
		if instances <= 0 || workers <= 0 || mean <= 0 || stddev < 0 {
			return nil, fmt.Errorf("compute capacity and service-time parameters are invalid")
		}
		pool := &ServerPool{Capacity: instances * workers, ServiceTime: LognormalSampler{MeanMs: mean, StdDevMs: stddev}}
		return &ComputeResource{ResourceID: id, Pool: pool, Downstream: downstream[0]}, nil
	case "database":
		if len(downstream) != 0 {
			return nil, fmt.Errorf("database cannot have a downstream resource in the MVP")
		}
		connections, err := intParameter(parameters, "max_connections")
		if err != nil {
			return nil, err
		}
		mean, err := floatParameter(parameters, "query_time_mean_ms")
		if err != nil {
			return nil, err
		}
		stddev, err := floatParameter(parameters, "query_time_stddev_ms")
		if err != nil {
			return nil, err
		}
		if connections <= 0 || mean <= 0 || stddev < 0 {
			return nil, fmt.Errorf("database capacity and query-time parameters are invalid")
		}
		return &DatabaseResource{ResourceID: id, ConnPool: &ConnectionPool{MaxConnections: connections}, QueryTime: LognormalSampler{MeanMs: mean, StdDevMs: stddev}}, nil
	default:
		return nil, fmt.Errorf("unknown simulation class: %s", simClass)
	}
}

func buildWorkload(config ir.WorkloadConfig) (workload.Generator, error) {
	segments := make([]workload.Generator, 0, len(config.Segments))
	for _, segment := range config.Segments {
		start := kernel.SimTime(segment.StartTimeS * float64(kernel.Second))
		end := kernel.SimTime(segment.EndTimeS * float64(kernel.Second))
		switch segment.Type {
		case "constant":
			segments = append(segments, workload.Constant{RatePerSec: segment.Rate, StartTime: start, EndTime: end})
		case "ramp":
			segments = append(segments, workload.Ramp{StartRate: segment.StartRate, EndRate: segment.EndRate, StartTime: start, EndTime: end})
		default:
			return nil, fmt.Errorf("unsupported workload type %q", segment.Type)
		}
	}
	return workload.NewComposite(segments), nil
}

func downstreamIDs(graph *ir.Graph, from ir.NodeID, ids map[ir.NodeID]kernel.ResourceID) []kernel.ResourceID {
	targets := make([]ir.NodeID, 0)
	for _, edge := range graph.Edges {
		if edge.From == from {
			targets = append(targets, edge.To)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	result := make([]kernel.ResourceID, 0, len(targets))
	for _, target := range targets {
		result = append(result, ids[target])
	}
	return result
}

func intParameter(parameters map[string]any, key string) (int, error) {
	switch value := parameters[key].(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case uint64:
		return int(value), nil
	case float64:
		return int(value), nil
	default:
		return 0, fmt.Errorf("parameter %s must be a number", key)
	}
}

func floatParameter(parameters map[string]any, key string) (float64, error) {
	switch value := parameters[key].(type) {
	case int:
		return float64(value), nil
	case int64:
		return float64(value), nil
	case uint64:
		return float64(value), nil
	case float64:
		return value, nil
	default:
		return 0, fmt.Errorf("parameter %s must be a number", key)
	}
}
