package report

import (
	"bytes"
	"strings"
	"testing"

	"archpulse/internal/analysis"
	"archpulse/internal/kernel"
	"archpulse/pkg/model"
)

func TestTextReportsBottleneckWithoutPlateau(t *testing.T) {
	result := model.RunResult{
		SamplingFactor: 2,
		Resources:      []model.ResourceDescriptor{{ResourceID: 1, NodeID: "api", Name: "Orders API", Type: "aws.ec2"}},
		Bottleneck:     analysis.BottleneckReport{Ranked: []analysis.ResourceVerdict{{Resource: 1, IsBottleneck: true, Classification: "root_constraint", Score: 91, Reason: "sustained queue"}}},
		Trace:          kernel.RunTrace{},
	}
	var output bytes.Buffer
	if err := Text(result, &output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "Throughput plateau: not detected") || !strings.Contains(text, "Primary bottleneck: Orders API (api, aws.ec2)") {
		t.Fatalf("incomplete report:\n%s", text)
	}
}
