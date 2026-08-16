package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

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

type JobProgress struct {
	VirtualTimeNS     kernel.SimTime     `json:"virtual_time_ns"`
	HorizonNS         kernel.SimTime     `json:"horizon_ns"`
	Percent           float64            `json:"percent"`
	EventsProcessed   uint64             `json:"events_processed"`
	CompletedRequests uint64             `json:"completed_requests"`
	QueuedRequests    int                `json:"queued_requests"`
	Resources         []ResourcePressure `json:"resources"`
}

type ResourcePressure struct {
	ResourceID     uint32  `json:"resource_id"`
	InFlight       int     `json:"in_flight"`
	QueueDepth     int     `json:"queue_depth"`
	Capacity       int     `json:"capacity"`
	UtilizationPct float64 `json:"utilization_pct"`
}

type JobResponse struct {
	SimulationID string              `json:"simulation_id"`
	Status       string              `json:"status"`
	Progress     JobProgress         `json:"progress"`
	Resources    []ResourceReference `json:"resources"`
	Result       *model.RunResult    `json:"result,omitempty"`
	Error        string              `json:"error,omitempty"`
}

type simulationJob struct {
	response        JobResponse
	context         context.Context
	cancel          context.CancelFunc
	cancelRequested atomic.Bool
}

type Server struct {
	mux               *http.ServeMux
	mu                sync.RWMutex
	jobs              map[string]*simulationJob
	slots             chan struct{}
	nextID            atomic.Uint64
	maxEvents         uint64
	maxQueuedRequests int
	runTimeout        time.Duration
}

const (
	webMaxEvents         = 8_000_000
	webMaxQueuedRequests = 250_000
	webRunTimeout        = 2 * time.Minute
)

func New() *Server {
	server := &Server{
		mux: http.NewServeMux(), jobs: make(map[string]*simulationJob), slots: make(chan struct{}, 2),
		maxEvents: webMaxEvents, maxQueuedRequests: webMaxQueuedRequests, runTimeout: webRunTimeout,
	}
	server.mux.HandleFunc("GET /api/health", server.health)
	server.mux.HandleFunc("GET /api/catalog", server.catalog)
	server.mux.HandleFunc("POST /api/architectures/import-yaml", server.importYAML)
	server.mux.HandleFunc("POST /api/validate", server.validate)
	server.mux.HandleFunc("POST /api/simulations", server.startSimulation)
	server.mux.HandleFunc("GET /api/simulations/{id}", server.simulationStatus)
	server.mux.HandleFunc("DELETE /api/simulations/{id}", server.cancelSimulation)
	server.mux.HandleFunc("POST /api/simulations/run", server.runSimulation)
	return server
}

func (s *Server) startSimulation(writer http.ResponseWriter, request *http.Request) {
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
	sink := engine.Metrics.(*metrics.Sink)
	ctx, cancel := context.WithTimeout(context.Background(), s.runTimeout)
	id := fmt.Sprintf("sim-%x-%x", time.Now().UnixMilli(), s.nextID.Add(1))
	job := &simulationJob{
		context: ctx,
		cancel:  cancel,
		response: JobResponse{
			SimulationID: id,
			Status:       "queued",
			Progress:     JobProgress{HorizonNS: engine.Horizon},
			Resources:    resourceReferences(input.Graph.Nodes),
		},
	}
	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()
	response := job.response
	go s.executeJob(job, engine, sink, input.Seed)
	writeJSON(writer, http.StatusAccepted, response)
}

func (s *Server) executeJob(job *simulationJob, engine *kernel.Engine, sink *metrics.Sink, seed int64) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-job.context.Done():
		s.finishCancelledOrTimedOut(job)
		return
	}
	s.mu.Lock()
	job.response.Status = "running"
	s.mu.Unlock()
	engine.MaxEvents = s.maxEvents
	engine.MaxQueuedRequests = s.maxQueuedRequests
	engine.ShouldStop = func() bool { return job.context.Err() != nil }
	engine.OnProgress = func(snapshot kernel.ProgressSnapshot) {
		progress := JobProgress{
			VirtualTimeNS:     snapshot.VirtualTime,
			HorizonNS:         snapshot.Horizon,
			EventsProcessed:   snapshot.EventsProcessed,
			CompletedRequests: sink.Global().Latency.Count(),
			QueuedRequests:    snapshot.QueuedRequests,
			Resources:         make([]ResourcePressure, 0, len(snapshot.Resources)),
		}
		if snapshot.Horizon > 0 {
			progress.Percent = min(100, 100*float64(snapshot.VirtualTime)/float64(snapshot.Horizon))
		}
		for _, resource := range snapshot.Resources {
			progress.Resources = append(progress.Resources, ResourcePressure{ResourceID: uint32(resource.ResourceID), InFlight: resource.InFlight, QueueDepth: resource.QueueDepth, Capacity: resource.Capacity, UtilizationPct: resource.UtilizationPct})
		}
		s.mu.Lock()
		job.response.Progress = progress
		s.mu.Unlock()
	}
	trace, err := engine.Run()
	if err != nil {
		if errors.Is(err, kernel.ErrSimulationCancelled) {
			s.finishCancelledOrTimedOut(job)
			return
		}
		s.mu.Lock()
		job.response.Status = "failed"
		job.response.Error = err.Error()
		s.mu.Unlock()
		job.cancel()
		return
	}
	result := model.NewRunResult(seed, trace, sink, analysis.Analyze(trace, sink))
	s.mu.Lock()
	job.response.Status = "completed"
	job.response.Result = &result
	job.response.Progress.Percent = 100
	job.response.Progress.VirtualTimeNS = trace.FinalTime
	job.response.Progress.EventsProcessed = trace.TotalEventsProcessed
	job.response.Progress.CompletedRequests = result.Latency.Count
	s.mu.Unlock()
	job.cancel()
}

func (s *Server) finishCancelledOrTimedOut(job *simulationJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job.cancelRequested.Load() {
		job.response.Status = "cancelled"
		job.response.Error = "simulation cancelled by user"
	} else {
		job.response.Status = "failed"
		job.response.Error = fmt.Sprintf("simulation exceeded the %s wall-clock limit", s.runTimeout)
	}
}

func (s *Server) simulationStatus(writer http.ResponseWriter, request *http.Request) {
	s.mu.RLock()
	job, exists := s.jobs[request.PathValue("id")]
	if !exists {
		s.mu.RUnlock()
		writeError(writer, http.StatusNotFound, fmt.Errorf("simulation not found"))
		return
	}
	response := job.response
	s.mu.RUnlock()
	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) cancelSimulation(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	job, exists := s.jobs[request.PathValue("id")]
	if !exists {
		s.mu.Unlock()
		writeError(writer, http.StatusNotFound, fmt.Errorf("simulation not found"))
		return
	}
	if job.response.Status == "completed" || job.response.Status == "failed" || job.response.Status == "cancelled" {
		response := job.response
		s.mu.Unlock()
		writeJSON(writer, http.StatusConflict, map[string]any{"simulation_id": response.SimulationID, "status": response.Status, "error": "simulation is already finished"})
		return
	}
	id := job.response.SimulationID
	s.mu.Unlock()
	job.cancelRequested.Store(true)
	job.cancel()
	writeJSON(writer, http.StatusAccepted, map[string]any{"simulation_id": id, "status": "cancelling"})
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
	ctx, cancel := context.WithTimeout(request.Context(), s.runTimeout)
	defer cancel()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		writeError(writer, http.StatusServiceUnavailable, fmt.Errorf("simulation capacity unavailable"))
		return
	}
	engine.MaxEvents = s.maxEvents
	engine.MaxQueuedRequests = s.maxQueuedRequests
	engine.ShouldStop = func() bool { return ctx.Err() != nil }
	trace, err := engine.Run()
	if err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)
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

func resourceReferences(nodes []ir.Node) []ResourceReference {
	resources := make([]ResourceReference, 0, len(nodes))
	for index, node := range nodes {
		resources = append(resources, ResourceReference{ResourceID: uint32(index), NodeID: node.ID, Type: node.ResourceType})
	}
	return resources
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
