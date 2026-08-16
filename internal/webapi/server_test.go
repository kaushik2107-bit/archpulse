package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	enginerunner "archpulse/internal/engine"
	"archpulse/internal/ir"
)

func TestCatalogAndSimulationEndpoints(t *testing.T) {
	server := httptest.NewServer(New().Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/catalog")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("catalog status = %d", response.StatusCode)
	}
	response.Body.Close()

	request := RunRequest{
		ArchitectureRequest: ArchitectureRequest{
			Graph: ir.Graph{
				Nodes: []ir.Node{
					{ID: "loadgen", ResourceType: "load_generator", Parameters: map[string]any{}},
					{ID: "alb", ResourceType: "aws.alb", Parameters: map[string]any{}},
					{ID: "api", ResourceType: "aws.ec2", Parameters: map[string]any{}},
					{ID: "database", ResourceType: "aws.rds.postgres", Parameters: map[string]any{}},
				},
				Edges: []ir.Edge{{From: "loadgen", To: "alb"}, {From: "alb", To: "api"}, {From: "api", To: "database"}},
			},
			Workload: ir.WorkloadConfig{Segments: []ir.WorkloadSegment{{Type: "constant", Rate: 50, StartTimeS: 0, EndTimeS: 1}}},
		},
		Seed: 42,
	}
	body, _ := json.Marshal(request)
	response, err = http.Post(server.URL+"/api/simulations/run", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("simulation status = %d", response.StatusCode)
	}
	var output RunResponse
	if err := json.NewDecoder(response.Body).Decode(&output); err != nil {
		t.Fatal(err)
	}
	if output.Result.Latency.Count == 0 || len(output.Resources) != 4 {
		t.Fatalf("unexpected simulation output: requests=%d resources=%d", output.Result.Latency.Count, len(output.Resources))
	}
	if len(output.Result.ResourceTimelines) != 4 || len(output.Result.ResourceTimelines[0].Utilization) == 0 {
		t.Fatalf("playback timelines missing: %+v", output.Result.ResourceTimelines)
	}
}

func TestImportYAMLEndpoint(t *testing.T) {
	server := httptest.NewServer(New().Handler())
	defer server.Close()
	yaml := `
services:
  loadgen: {type: load_generator}
  alb: {type: aws.alb}
  api: {type: aws.ec2}
  database: {type: aws.rds.postgres}
connections:
  - {from: loadgen, to: alb}
  - {from: alb, to: api}
  - {from: api, to: database}
workload:
  - {type: constant, rate: 100, start_time_s: 0, end_time_s: 10}
`
	response, err := http.Post(server.URL+"/api/architectures/import-yaml", "application/yaml", bytes.NewBufferString(yaml))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("import status = %d", response.StatusCode)
	}
	var imported ArchitectureRequest
	if err := json.NewDecoder(response.Body).Decode(&imported); err != nil {
		t.Fatal(err)
	}
	if len(imported.Graph.Nodes) != 4 || len(imported.Graph.Edges) != 3 || len(imported.Workload.Segments) != 1 {
		t.Fatalf("unexpected import: nodes=%d edges=%d segments=%d", len(imported.Graph.Nodes), len(imported.Graph.Edges), len(imported.Workload.Segments))
	}
}

func TestAsynchronousSimulationLifecycle(t *testing.T) {
	server := httptest.NewServer(New().Handler())
	defer server.Close()
	request := RunRequest{
		ArchitectureRequest: ArchitectureRequest{
			Graph: ir.Graph{
				Nodes: []ir.Node{{ID: "loadgen", ResourceType: "load_generator", Parameters: map[string]any{}}, {ID: "alb", ResourceType: "aws.alb", Parameters: map[string]any{}}, {ID: "api", ResourceType: "aws.ec2", Parameters: map[string]any{}}, {ID: "database", ResourceType: "aws.rds.postgres", Parameters: map[string]any{}}},
				Edges: []ir.Edge{{From: "loadgen", To: "alb"}, {From: "alb", To: "api"}, {From: "api", To: "database"}},
			},
			Workload: ir.WorkloadConfig{Segments: []ir.WorkloadSegment{{Type: "constant", Rate: 50, StartTimeS: 0, EndTimeS: 2}}},
		},
		Seed: 42,
	}
	body, _ := json.Marshal(request)
	response, err := http.Post(server.URL+"/api/simulations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d", response.StatusCode)
	}
	var job JobResponse
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get(server.URL + "/api/simulations/" + job.SimulationID)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if job.Status == "completed" {
			if job.Result == nil || job.Progress.Percent != 100 || job.Progress.CompletedRequests == 0 || len(job.Result.ResourceTimelines) != 4 {
				t.Fatalf("incomplete completed job: %+v", job)
			}
			return
		}
		if job.Status == "failed" || job.Status == "cancelled" {
			t.Fatalf("job ended as %s: %s", job.Status, job.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job did not complete; last status %s", job.Status)
}

func TestSafetyLimitFailsJobWithoutCrashingServer(t *testing.T) {
	api := New()
	api.maxEvents = 4096
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	request := RunRequest{
		ArchitectureRequest: ArchitectureRequest{
			Graph: ir.Graph{
				Nodes: []ir.Node{{ID: "loadgen", ResourceType: "load_generator", Parameters: map[string]any{}}, {ID: "alb", ResourceType: "aws.alb", Parameters: map[string]any{}}, {ID: "api", ResourceType: "aws.ec2", Parameters: map[string]any{}}, {ID: "database", ResourceType: "aws.rds.postgres", Parameters: map[string]any{}}},
				Edges: []ir.Edge{{From: "loadgen", To: "alb"}, {From: "alb", To: "api"}, {From: "api", To: "database"}},
			},
			Workload: ir.WorkloadConfig{Segments: []ir.WorkloadSegment{{Type: "constant", Rate: 100_000, StartTimeS: 0, EndTimeS: 300}}},
		},
		Seed: 42,
	}
	body, _ := json.Marshal(request)
	response, err := http.Post(server.URL+"/api/simulations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var job JobResponse
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get(server.URL + "/api/simulations/" + job.SimulationID)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if job.Status == "failed" {
			if job.Error == "" {
				t.Fatal("failed job did not explain the safety limit")
			}
			health, err := http.Get(server.URL + "/api/health")
			if err != nil || health.StatusCode != http.StatusOK {
				t.Fatalf("server unhealthy after limited job: response=%v error=%v", health, err)
			}
			health.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("limited job did not fail; last status %s", job.Status)
}

func TestSampleArchitectureUsesAutomaticWeightedSampling(t *testing.T) {
	workload := ir.WorkloadConfig{Segments: []ir.WorkloadSegment{
		{Type: "constant", Rate: 5_000, StartTimeS: 0, EndTimeS: 60},
		{Type: "ramp", StartRate: 5_000, EndRate: 50_000, StartTimeS: 60, EndTimeS: 180},
		{Type: "constant", Rate: 50_000, StartTimeS: 180, EndTimeS: 300},
	}}
	estimated := enginerunner.EstimateArrivals(workload, 0)
	if estimated != 9_600_000 {
		t.Fatalf("estimated arrivals = %.0f, want 9600000", estimated)
	}
	if factor := enginerunner.RecommendedTrafficScale(estimated); factor != 48 {
		t.Fatalf("sampling factor = %d, want 48", factor)
	}
}
