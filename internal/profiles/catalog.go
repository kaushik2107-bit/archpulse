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

func clone(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
