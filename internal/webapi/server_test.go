package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"infra-sim/internal/ir"
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
