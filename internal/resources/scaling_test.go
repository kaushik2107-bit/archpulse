package resources

import (
	"testing"

	"infra-sim/internal/ir"
)

func TestBuildWorldScaledReducesStoredCapacityAndPreservesReportedCapacity(t *testing.T) {
	graph := &ir.Graph{
		Nodes: []ir.Node{
			{ID: "loadgen", ResourceType: "load_generator", Parameters: map[string]any{}},
			{ID: "alb", ResourceType: "aws.alb", Parameters: map[string]any{}},
			{ID: "api", ResourceType: "aws.ec2", Parameters: map[string]any{"instances": 4}},
			{ID: "database", ResourceType: "aws.rds.postgres", Parameters: map[string]any{}},
		},
		Edges: []ir.Edge{{From: "loadgen", To: "alb"}, {From: "alb", To: "api"}, {From: "api", To: "database"}},
	}
	workload := ir.WorkloadConfig{Segments: []ir.WorkloadSegment{{Type: "constant", Rate: 10_000, StartTimeS: 0, EndTimeS: 10}}}
	world, ids, err := BuildWorldScaled(graph, workload, 20)
	if err != nil {
		t.Fatal(err)
	}
	loadGenerator := world.Get(ids["loadgen"]).(*LoadGenerator)
	compute := world.Get(ids["api"]).(*ComputeResource)
	database := world.Get(ids["database"]).(*DatabaseResource)
	if loadGenerator.RequestWeight != 20 {
		t.Fatalf("request weight = %d, want 20", loadGenerator.RequestWeight)
	}
	if compute.Pool.Capacity != 20 || compute.SnapshotMetrics().Capacity != 400 {
		t.Fatalf("compute capacities stored=%d reported=%d", compute.Pool.Capacity, compute.SnapshotMetrics().Capacity)
	}
	if database.ConnPool.MaxConnections != 10 || database.SnapshotMetrics().Capacity != 200 {
		t.Fatalf("database capacities stored=%d reported=%d", database.ConnPool.MaxConnections, database.SnapshotMetrics().Capacity)
	}
}
