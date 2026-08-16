package engine

import (
	"fmt"

	"infra-sim/internal/failure"
	"infra-sim/internal/ir"
	"infra-sim/internal/kernel"
	"infra-sim/internal/metrics"
	"infra-sim/internal/resources"
)

func Bootstrap(graph *ir.Graph, workloadConfig ir.WorkloadConfig, failureConfig []ir.FailureConfig, seed int64) (*kernel.Engine, error) {
	return BootstrapWithTrafficScale(graph, workloadConfig, failureConfig, seed, 1)
}

func BootstrapWithTrafficScale(graph *ir.Graph, workloadConfig ir.WorkloadConfig, failureConfig []ir.FailureConfig, seed int64, trafficScale int) (*kernel.Engine, error) {
	world, ids, err := resources.BuildWorldScaled(graph, workloadConfig, trafficScale)
	if err != nil {
		return nil, fmt.Errorf("build world: %w", err)
	}
	loadGeneratorNode, entryPointNode, err := findEntryPoint(graph)
	if err != nil {
		return nil, err
	}
	queue := kernel.NewEventQueue()
	queue.Push(kernel.Event{Time: 0, Type: kernel.RequestArrived, Target: ids[loadGeneratorNode]})
	queue.Push(kernel.Event{Time: 0, Type: kernel.PolicyTick})
	for _, config := range failureConfig {
		failure.ScheduledFailure{
			At:                kernel.SimTime(config.AtS * float64(kernel.Second)),
			Target:            ids[config.Target],
			LatencyMultiplier: config.LatencyMultiplier,
			Instance:          config.Instance,
		}.Seed(queue)
	}
	last := workloadConfig.Segments[len(workloadConfig.Segments)-1]
	return &kernel.Engine{
		Queue:   queue,
		World:   world,
		RNG:     kernel.NewRNGStreams(seed),
		Metrics: metrics.NewSink(world, ids[entryPointNode], kernel.Second),
		Horizon: kernel.SimTime(last.EndTimeS * float64(kernel.Second)),
	}, nil
}

func findEntryPoint(graph *ir.Graph) (ir.NodeID, ir.NodeID, error) {
	var loadGenerator ir.NodeID
	for _, node := range graph.Nodes {
		if node.ResourceType == "load_generator" {
			loadGenerator = node.ID
			break
		}
	}
	for _, edge := range graph.Edges {
		if edge.From == loadGenerator {
			return loadGenerator, edge.To, nil
		}
	}
	return "", "", fmt.Errorf("load generator has no entry-point connection")
}
