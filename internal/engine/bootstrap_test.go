package engine

import (
	"testing"

	"infra-sim/internal/ir"
	"infra-sim/internal/metrics"
)

const testArchitecture = `
services:
  loadgen: {type: load_generator}
  alb: {type: aws.alb}
  api:
    type: aws.ec2
    instances: 1
    workers_per_instance: 5
    service_time_mean_ms: 2
    service_time_stddev_ms: 0
  database:
    type: aws.rds.postgres
    max_connections: 2
    query_time_mean_ms: 5
    query_time_stddev_ms: 0
connections:
  - {from: loadgen, to: alb}
  - {from: alb, to: api}
  - {from: api, to: database}
workload:
  - {type: constant, rate: 100, start_time_s: 0, end_time_s: 3}
`

func TestBootstrapRunIsDeterministic(t *testing.T) {
	run := func() (uint64, uint64, float64) {
		graph, workload, failures, err := ir.CompileYAML([]byte(testArchitecture))
		if err != nil {
			t.Fatal(err)
		}
		engine, err := Bootstrap(graph, workload, failures, 42)
		if err != nil {
			t.Fatal(err)
		}
		trace, err := engine.Run()
		if err != nil {
			t.Fatal(err)
		}
		sink := engine.Metrics.(*metrics.Sink)
		return trace.TotalEventsProcessed, sink.Global().Latency.Count(), sink.Global().Latency.Quantile(99)
	}
	events1, requests1, p991 := run()
	events2, requests2, p992 := run()
	if events1 != events2 || requests1 != requests2 || p991 != p992 {
		t.Fatalf("runs differ: (%d, %d, %f) != (%d, %d, %f)", events1, requests1, p991, events2, requests2, p992)
	}
	if requests1 == 0 {
		t.Fatal("simulation completed no requests")
	}
}
