package webapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"infra-sim/internal/analysis"
	enginerunner "infra-sim/internal/engine"
	"infra-sim/internal/ir"
	"infra-sim/internal/kernel"
	"infra-sim/internal/metrics"
	"infra-sim/internal/profiles"
	"infra-sim/internal/resources"
	"infra-sim/pkg/model"
)

type ArchitectureRequest struct {
	Graph    ir.Graph           `json:"graph"`
	Workload ir.WorkloadConfig  `json:"workload"`
	Failures []ir.FailureConfig `json:"failures"`
}

type RunRequest struct {
	ArchitectureRequest
	Seed      int64   `json:"seed"`
	DurationS float64 `json:"duration_s,omitempty"`
}

type ResourceReference struct {
	ResourceID uint32    `json:"resource_id"`
	NodeID     ir.NodeID `json:"node_id"`
	Type       string    `json:"type"`
}

type RunResponse struct {
	Result    model.RunResult     `json:"result"`
	Resources []ResourceReference `json:"resources"`
}

type Server struct{ mux *http.ServeMux }

func New() *Server {
	server := &Server{mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /api/health", server.health)
	server.mux.HandleFunc("GET /api/catalog", server.catalog)
	server.mux.HandleFunc("POST /api/architectures/import-yaml", server.importYAML)
	server.mux.HandleFunc("POST /api/validate", server.validate)
	server.mux.HandleFunc("POST /api/simulations/run", server.runSimulation)
	return server
}

func (s *Server) importYAML(writer http.ResponseWriter, request *http.Request) {
	data, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
	if err != nil {
		writeError(writer, http.StatusBadRequest, fmt.Errorf("read yaml: %w", err))
		return
	}
	graph, workload, failures, err := ir.CompileYAML(data)
	if err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)
		return
	}
	result := ArchitectureRequest{Graph: *graph, Workload: workload, Failures: failures}
	if err := validateArchitecture(result); err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Cache-Control", "no-store")
		s.mux.ServeHTTP(writer, request)
	})
}

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) catalog(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"services": profiles.CatalogForEditor()})
}

func (s *Server) validate(writer http.ResponseWriter, request *http.Request) {
	var input ArchitectureRequest
	if err := decodeJSON(request.Body, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if err := validateArchitecture(input); err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"valid": true})
}

func (s *Server) runSimulation(writer http.ResponseWriter, request *http.Request) {
	var input RunRequest
	if err := decodeJSON(request.Body, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if err := validateArchitecture(input.ArchitectureRequest); err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)
		return
	}
	engine, err := enginerunner.Bootstrap(&input.Graph, input.Workload, input.Failures, input.Seed)
	if err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)
		return
	}
	if input.DurationS > 0 {
		engine.Horizon = timeToSimTime(input.DurationS)
	}
	trace, err := engine.Run()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	sink, ok := engine.Metrics.(*metrics.Sink)
	if !ok {
		writeError(writer, http.StatusInternalServerError, fmt.Errorf("unexpected metrics sink"))
		return
	}
	result := model.NewRunResult(input.Seed, trace, sink, analysis.Analyze(trace, sink))
	resources := make([]ResourceReference, 0, len(input.Graph.Nodes))
	for index, node := range input.Graph.Nodes {
		resources = append(resources, ResourceReference{ResourceID: uint32(index), NodeID: node.ID, Type: node.ResourceType})
	}
	writeJSON(writer, http.StatusOK, RunResponse{Result: result, Resources: resources})
}

func validateArchitecture(input ArchitectureRequest) error {
	if err := ir.Validate(&input.Graph, input.Workload, input.Failures); err != nil {
		return err
	}
	_, _, err := resources.BuildWorld(&input.Graph, input.Workload)
	return err
}

func timeToSimTime(seconds float64) kernel.SimTime {
	return kernel.SimTime(seconds * float64(kernel.Second))
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	return nil
}

func writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
