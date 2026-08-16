package report

import (
	"encoding/json"
	"fmt"
	"io"

	"infra-sim/internal/kernel"
	"infra-sim/pkg/model"
)

func JSON(result model.RunResult, writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func Text(result model.RunResult, writer io.Writer) error {
	samplingFactor := max(1, result.SamplingFactor)
	if _, err := fmt.Fprintf(writer, "Events processed: %d\nRequests served: %d\nLatency: mean %.1f us, p50 %.1f us, p95 %.1f us, p99 %.1f us\nRejected: %d\nSampling: %dx (%d estimated arrivals)\n", result.Trace.TotalEventsProcessed, result.Latency.Count, result.Latency.Mean, result.Latency.P50, result.Latency.P95, result.Latency.P99, result.Rejected, samplingFactor, result.EstimatedArrivals); err != nil {
		return err
	}
	if result.Bottleneck.PlateauStartTime == 0 {
		if _, err := fmt.Fprintln(writer, "Throughput plateau: not detected"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(writer, "Throughput plateau: %.0f RPS at %.1fs\n", result.Bottleneck.PlateauThroughput, float64(result.Bottleneck.PlateauStartTime)/1e9); err != nil {
			return err
		}
	}
	if drops := result.Bottleneck.ThroughputDrops; len(drops) > 0 {
		largest := drops[0]
		for _, drop := range drops[1:] {
			if drop.DropPct > largest.DropPct {
				largest = drop
			}
		}
		if _, err := fmt.Fprintf(writer, "Largest throughput drop: %.1f%% at %.1fs (%.0f -> %.0f RPS)\n", largest.DropPct, float64(largest.Time)/1e9, largest.BeforeRPS, largest.AfterRPS); err != nil {
			return err
		}
	}
	found := false
	for _, verdict := range result.Bottleneck.Ranked {
		if verdict.IsBottleneck {
			label := resourceLabel(result, verdict.Resource)
			prefix := "Contributing constraint"
			if !found {
				prefix = "Primary bottleneck"
			}
			if _, err := fmt.Fprintf(writer, "%s: %s [%s, score %.1f]\n  %s\n", prefix, label, verdict.Classification, verdict.Score, verdict.Reason); err != nil {
				return err
			}
			found = true
		}
	}
	if !found {
		_, err := fmt.Fprintln(writer, "Bottleneck: no sustained capacity constraint detected")
		return err
	}
	return nil
}

func resourceLabel(result model.RunResult, id kernel.ResourceID) string {
	for _, resource := range result.Resources {
		if resource.ResourceID != id {
			continue
		}
		if resource.Name != "" {
			return fmt.Sprintf("%s (%s, %s)", resource.Name, resource.NodeID, resource.Type)
		}
		return fmt.Sprintf("%s (%s)", resource.NodeID, resource.Type)
	}
	return fmt.Sprintf("resource %d", id)
}
