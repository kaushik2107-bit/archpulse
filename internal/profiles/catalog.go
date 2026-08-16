package profiles

import (
	"fmt"
	"sort"
)

type DisplayMeta struct {
	Icon     string
	Label    string
	Category string
}

type ServiceProfile struct {
	AWSType  string
	SimClass string
	Defaults map[string]any
	Display  DisplayMeta
}

type VisualizerEntry struct {
	Type     string `json:"type"`
	Icon     string `json:"icon"`
	Label    string `json:"label"`
	Category string `json:"category"`
}

type ConfigurationField struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Kind  string  `json:"kind"`
	Min   float64 `json:"min,omitempty"`
	Step  float64 `json:"step,omitempty"`
	Unit  string  `json:"unit,omitempty"`
}

type CatalogEntry struct {
	Type       string               `json:"type"`
	Icon       string               `json:"icon"`
	Label      string               `json:"label"`
	Category   string               `json:"category"`
	Defaults   map[string]any       `json:"defaults"`
	Parameters []ConfigurationField `json:"parameters"`
}

var catalog = map[string]ServiceProfile{
	"aws.alb":          {AWSType: "aws.alb", SimClass: "load_balancer", Defaults: map[string]any{}, Display: DisplayMeta{Icon: "alb", Label: "Application Load Balancer", Category: "networking"}},
	"aws.ec2":          {AWSType: "aws.ec2", SimClass: "compute", Defaults: map[string]any{"instances": 1, "workers_per_instance": 100, "service_time_mean_ms": 8.0, "service_time_stddev_ms": 3.0}, Display: DisplayMeta{Icon: "ec2", Label: "EC2", Category: "compute"}},
	"aws.ecs":          {AWSType: "aws.ecs", SimClass: "compute", Defaults: map[string]any{"instances": 1, "workers_per_instance": 50, "service_time_mean_ms": 8.0, "service_time_stddev_ms": 3.0}, Display: DisplayMeta{Icon: "ecs", Label: "ECS", Category: "compute"}},
	"aws.rds.postgres": {AWSType: "aws.rds.postgres", SimClass: "database", Defaults: map[string]any{"max_connections": 200, "query_time_mean_ms": 6.0, "query_time_stddev_ms": 2.0}, Display: DisplayMeta{Icon: "rds", Label: "RDS PostgreSQL", Category: "database"}},
}

func Resolve(resourceType string, userParameters map[string]any) (string, map[string]any, error) {
	if resourceType == "load_generator" {
		return resourceType, clone(userParameters), nil
	}
	profile, exists := catalog[resourceType]
	if !exists {
		return "", nil, fmt.Errorf("unknown AWS service type: %s", resourceType)
	}
	merged := clone(profile.Defaults)
	for key, value := range userParameters {
		merged[key] = value
	}
	return profile.SimClass, merged, nil
}

func ListForVisualizer() []VisualizerEntry {
	entries := make([]VisualizerEntry, 0, len(catalog))
	for resourceType, profile := range catalog {
		entries = append(entries, VisualizerEntry{Type: resourceType, Icon: profile.Display.Icon, Label: profile.Display.Label, Category: profile.Display.Category})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Type < entries[j].Type })
	return entries
}

func CatalogForEditor() []CatalogEntry {
	entries := []CatalogEntry{{Type: "load_generator", Icon: "pulse", Label: "Load Generator", Category: "traffic", Defaults: map[string]any{}}}
	fields := map[string][]ConfigurationField{
		"aws.ec2": {
			{Key: "instances", Label: "Instances", Kind: "integer", Min: 1, Step: 1},
			{Key: "workers_per_instance", Label: "Workers per instance", Kind: "integer", Min: 1, Step: 1},
			{Key: "service_time_mean_ms", Label: "Mean service time", Kind: "number", Min: 0.01, Step: 0.1, Unit: "ms"},
			{Key: "service_time_stddev_ms", Label: "Service-time deviation", Kind: "number", Min: 0, Step: 0.1, Unit: "ms"},
		},
		"aws.ecs": {
			{Key: "instances", Label: "Tasks", Kind: "integer", Min: 1, Step: 1},
			{Key: "workers_per_instance", Label: "Workers per task", Kind: "integer", Min: 1, Step: 1},
			{Key: "service_time_mean_ms", Label: "Mean service time", Kind: "number", Min: 0.01, Step: 0.1, Unit: "ms"},
			{Key: "service_time_stddev_ms", Label: "Service-time deviation", Kind: "number", Min: 0, Step: 0.1, Unit: "ms"},
		},
		"aws.rds.postgres": {
			{Key: "max_connections", Label: "Max connections", Kind: "integer", Min: 1, Step: 1},
			{Key: "query_time_mean_ms", Label: "Mean query time", Kind: "number", Min: 0.01, Step: 0.1, Unit: "ms"},
			{Key: "query_time_stddev_ms", Label: "Query-time deviation", Kind: "number", Min: 0, Step: 0.1, Unit: "ms"},
		},
	}
	keys := make([]string, 0, len(catalog))
	for key := range catalog {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		profile := catalog[key]
		entries = append(entries, CatalogEntry{Type: key, Icon: profile.Display.Icon, Label: profile.Display.Label, Category: profile.Display.Category, Defaults: clone(profile.Defaults), Parameters: fields[key]})
	}
	return entries
}

func clone(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
