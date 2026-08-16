package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"infra-sim/internal/analysis"
	enginerunner "infra-sim/internal/engine"
	"infra-sim/internal/ir"
	"infra-sim/internal/kernel"
	"infra-sim/internal/metrics"
	"infra-sim/internal/report"
	"infra-sim/pkg/model"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "validate":
		if len(args) != 2 {
			return fmt.Errorf("usage: infra-sim validate <architecture.yaml>")
		}
		_, _, _, err := compileFile(args[1])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "architecture is valid")
		return err
	case "run":
		return runSimulation(args[1:], stdout)
	case "report":
		if len(args) != 2 {
			return fmt.Errorf("usage: infra-sim report <result.json>")
		}
		file, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer file.Close()
		var result model.RunResult
		if err := json.NewDecoder(file).Decode(&result); err != nil {
			return fmt.Errorf("decode result: %w", err)
		}
		return report.Text(result, stdout)
	default:
		return usageError()
	}
}

func runSimulation(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	seed := flags.Int64("seed", 42, "master random seed")
	out := flags.String("out", "", "write JSON result to this file")
	jsonOutput := flags.Bool("json", false, "print JSON instead of text")
	traffic := flags.Float64("traffic", 0, "replace workload with a constant request rate")
	duration := flags.String("duration", "", "override simulation horizon, for example 30s or 5m")
	architecture, flagArgs, err := architectureAndFlags(args)
	if err != nil {
		return err
	}
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	graph, workloadConfig, failureConfig, err := compileFile(architecture)
	if err != nil {
		return err
	}
	if *traffic > 0 {
		end := workloadConfig.Segments[len(workloadConfig.Segments)-1].EndTimeS
		workloadConfig.Segments = []ir.WorkloadSegment{{Type: "constant", Rate: *traffic, StartTimeS: 0, EndTimeS: end}}
	}
	engine, err := enginerunner.Bootstrap(graph, workloadConfig, failureConfig, *seed)
	if err != nil {
		return err
	}
	if *duration != "" {
		parsed, err := time.ParseDuration(*duration)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("invalid duration %q", *duration)
		}
		engine.Horizon = kernel.SimTime(parsed.Nanoseconds())
	}
	trace, err := engine.Run()
	if err != nil {
		return err
	}
	sink, ok := engine.Metrics.(*metrics.Sink)
	if !ok {
		return errors.New("engine metrics sink has unexpected type")
	}
	result := model.NewRunResult(*seed, trace, sink, analysis.Analyze(trace, sink))
	if *out != "" {
		file, err := os.Create(*out)
		if err != nil {
			return err
		}
		if err := report.JSON(result, file); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	if *jsonOutput {
		return report.JSON(result, stdout)
	}
	return report.Text(result, stdout)
}

func architectureAndFlags(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("usage: infra-sim run <architecture.yaml> [flags]")
	}
	if args[0][0] != '-' {
		return args[0], args[1:], nil
	}
	// Support flags before the path by locating the final non-flag argument.
	for index := len(args) - 1; index >= 0; index-- {
		if args[index][0] != '-' {
			if _, err := strconv.ParseFloat(args[index], 64); err == nil && index > 0 {
				continue
			}
			return args[index], append(args[:index:index], args[index+1:]...), nil
		}
	}
	return "", nil, fmt.Errorf("architecture path is required")
}

func compileFile(path string) (*ir.Graph, ir.WorkloadConfig, []ir.FailureConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ir.WorkloadConfig{}, nil, err
	}
	return ir.CompileYAML(data)
}

func usageError() error {
	return fmt.Errorf("usage: infra-sim <validate|run|report> [arguments]")
}
