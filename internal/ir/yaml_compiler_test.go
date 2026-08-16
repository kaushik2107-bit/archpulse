package ir

import "testing"

func TestCompileYAMLSortsNodesAndAcceptsContiguousSegments(t *testing.T) {
	data := []byte(`
services:
  loadgen: {type: load_generator}
  database: {type: aws.rds.postgres}
  api: {type: aws.ec2, name: Orders API, instances: 2}
  alb: {type: aws.alb}
connections:
  - {from: loadgen, to: alb}
  - {from: alb, to: api}
  - {from: api, to: database}
workload:
  - {type: constant, rate: 100, start_time_s: 0, end_time_s: 10}
  - {type: ramp, start_rate: 100, end_rate: 200, start_time_s: 10, end_time_s: 20}
failures:
  - {target: api, instance: 2, at_s: 12, latency_multiplier: 3}
`)
	graph, workload, failures, err := CompileYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []NodeID{"alb", "api", "database", "loadgen"}
	for index := range want {
		if graph.Nodes[index].ID != want[index] {
			t.Fatalf("node %d = %q, want %q", index, graph.Nodes[index].ID, want[index])
		}
	}
	if len(workload.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(workload.Segments))
	}
	if graph.Nodes[1].Name != "Orders API" {
		t.Fatalf("node name = %q, want Orders API", graph.Nodes[1].Name)
	}
	if _, exists := graph.Nodes[1].Parameters["name"]; exists {
		t.Fatal("service name leaked into resource parameters")
	}
	if len(failures) != 1 || failures[0].Instance != 2 {
		t.Fatalf("replica failure was not compiled: %+v", failures)
	}
}

func TestCompileYAMLRejectsWorkloadGap(t *testing.T) {
	data := []byte(`
services:
  loadgen: {type: load_generator}
workload:
  - {type: constant, rate: 1, start_time_s: 0, end_time_s: 10}
  - {type: constant, rate: 1, start_time_s: 11, end_time_s: 20}
`)
	if _, _, _, err := CompileYAML(data); err == nil {
		t.Fatal("expected non-contiguous workload error")
	}
}
