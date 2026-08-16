package report

import (
	"encoding/json"
	"fmt"
	"io"

	"infra-sim/pkg/model"
)

func JSON(result model.RunResult, writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func Text(result model.RunResult, writer io.Writer) error {
	if _, err := fmt.Fprintf(writer, "Events processed: %d\nCompleted requests: %d\nLatency: mean %.1f us, p50 %.1f us, p95 %.1f us, p99 %.1f us\nRejected: %d\n", result.Trace.TotalEventsProcessed, result.Latency.Count, result.Latency.Mean, result.Latency.P50, result.Latency.P95, result.Latency.P99, result.Rejected); err != nil {
		return err
	}
	if result.Bottleneck.PlateauStartTime == 0 {
		_, err := fmt.Fprintln(writer, "Throughput plateau: not detected")
		return err
	}
	if _, err := fmt.Fprintf(writer, "Throughput plateau: %.0f RPS at %.1fs\n", result.Bottleneck.PlateauThroughput, float64(result.Bottleneck.PlateauStartTime)/1e9); err != nil {
		return err
	}
	for _, verdict := range result.Bottleneck.Ranked {
		if verdict.IsBottleneck {
			_, err := fmt.Fprintf(writer, "Bottleneck resource %d: %s (utilization %.1f%%, queue %.0f)\n", verdict.Resource, verdict.Reason, verdict.UtilizationPct, verdict.QueueDepth)
			return err
		}
	}
	_, err := fmt.Fprintln(writer, "Bottleneck: no sustained queue growth detected")
	return err
}
