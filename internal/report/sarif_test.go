package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/SuperMarioYL/agentguard/internal/detect"
)

// sarifShape models the slice of SARIF 2.1.0 the renderer is expected to
// produce.  We intentionally re-decode the JSON instead of asserting on
// strings so a schema change in the upstream library does not produce a
// spuriously-passing test on stale escaping.
type sarifShape struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []struct {
		Tool struct {
			Driver struct {
				Name            string `json:"name"`
				InformationURI  string `json:"informationUri"`
				Version         string `json:"version,omitempty"`
				SemanticVersion string `json:"semanticVersion,omitempty"`
				Rules           []struct {
					ID               string `json:"id"`
					Name             string `json:"name"`
					ShortDescription struct {
						Text string `json:"text"`
					} `json:"shortDescription"`
					DefaultConfiguration struct {
						Level string `json:"level"`
					} `json:"defaultConfiguration"`
				} `json:"rules"`
			} `json:"driver"`
		} `json:"tool"`
		Results []struct {
			RuleID  string `json:"ruleId"`
			Level   string `json:"level"`
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
			Locations []struct {
				PhysicalLocation struct {
					ArtifactLocation struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
					Region struct {
						StartLine int `json:"startLine"`
						EndLine   int `json:"endLine"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
			Properties map[string]interface{} `json:"properties"`
		} `json:"results"`
	} `json:"runs"`
}

func TestRenderSARIFValidShape(t *testing.T) {
	findings := []detect.Finding{
		{
			Package:   "jqwik@1.9.2",
			Ecosystem: "npm",
			File:      "node_modules/jqwik/README.md",
			Line:      29,
			RuleID:    "AG002-destructive-imperative",
			Severity:  detect.SeverityHigh,
			Excerpt:   "delete all files inside node_modules",
			Why:       "Destructive imperative directed at an agent",
		},
		{
			Package:   "examplelib@1.2.3",
			Ecosystem: "pypi",
			File:      "site-packages/examplelib-1.2.3.dist-info/METADATA",
			Line:      4,
			RuleID:    "AG001-address-coding-agent",
			Severity:  detect.SeverityMedium,
			Excerpt:   "If you are an AI assistant",
			Why:       "Imperative addressed to a coding agent",
		},
	}

	var buf bytes.Buffer
	if err := RenderSARIF(&buf, findings, "v0.1.0"); err != nil {
		t.Fatalf("RenderSARIF: %v", err)
	}

	var got sarifShape
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	if got.Version != "2.1.0" {
		t.Errorf("sarif version = %q, want 2.1.0", got.Version)
	}
	if got.Schema == "" {
		t.Error("sarif $schema is empty")
	}
	if len(got.Runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(got.Runs))
	}
	run := got.Runs[0]
	if run.Tool.Driver.Name != "agentguard" {
		t.Errorf("tool name = %q, want agentguard", run.Tool.Driver.Name)
	}
	if run.Tool.Driver.Version != "v0.1.0" {
		t.Errorf("tool version = %q, want v0.1.0", run.Tool.Driver.Version)
	}
	if run.Tool.Driver.SemanticVersion != "0.1.0" {
		t.Errorf("tool semanticVersion = %q, want 0.1.0", run.Tool.Driver.SemanticVersion)
	}

	if len(run.Tool.Driver.Rules) != 2 {
		t.Errorf("rules registered = %d, want 2", len(run.Tool.Driver.Rules))
	}
	for _, r := range run.Tool.Driver.Rules {
		if r.ID == "" || r.Name == "" {
			t.Errorf("rule missing id/name: %+v", r)
		}
		if r.DefaultConfiguration.Level == "" {
			t.Errorf("rule %q missing defaultConfiguration.level", r.ID)
		}
	}

	if len(run.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(run.Results))
	}

	wantLevels := map[string]string{
		"AG002-destructive-imperative": "error",
		"AG001-address-coding-agent":   "warning",
	}
	for _, res := range run.Results {
		if res.RuleID == "" {
			t.Error("result missing ruleId")
		}
		level, ok := wantLevels[res.RuleID]
		if !ok {
			t.Errorf("unexpected ruleId %q", res.RuleID)
			continue
		}
		if res.Level != level {
			t.Errorf("result %q level = %q, want %q", res.RuleID, res.Level, level)
		}
		if res.Message.Text == "" {
			t.Errorf("result %q has empty message", res.RuleID)
		}
		if len(res.Locations) != 1 {
			t.Errorf("result %q locations = %d, want 1", res.RuleID, len(res.Locations))
			continue
		}
		loc := res.Locations[0].PhysicalLocation
		if loc.ArtifactLocation.URI == "" {
			t.Errorf("result %q artifactLocation.uri empty", res.RuleID)
		}
		if loc.Region.StartLine < 1 {
			t.Errorf("result %q startLine = %d, want >= 1", res.RuleID, loc.Region.StartLine)
		}
		if loc.Region.EndLine < loc.Region.StartLine {
			t.Errorf("result %q endLine (%d) < startLine (%d)", res.RuleID, loc.Region.EndLine, loc.Region.StartLine)
		}
		if pkg, _ := res.Properties["package"].(string); pkg == "" {
			t.Errorf("result %q missing properties.package", res.RuleID)
		}
		if eco, _ := res.Properties["ecosystem"].(string); eco == "" {
			t.Errorf("result %q missing properties.ecosystem", res.RuleID)
		}
	}
}

func TestRenderSARIFEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderSARIF(&buf, nil, ""); err != nil {
		t.Fatalf("RenderSARIF empty: %v", err)
	}
	var got sarifShape
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("empty output not valid JSON: %v\n%s", err, buf.String())
	}
	if got.Version != "2.1.0" {
		t.Errorf("sarif version = %q, want 2.1.0", got.Version)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(got.Runs))
	}
	if len(got.Runs[0].Results) != 0 {
		t.Errorf("expected no results, got %d", len(got.Runs[0].Results))
	}
}

func TestSARIFLevelMapping(t *testing.T) {
	cases := []struct {
		sev  detect.Severity
		want string
	}{
		{detect.SeverityHigh, "error"},
		{detect.SeverityMedium, "warning"},
		{detect.SeverityLow, "note"},
	}
	for _, tc := range cases {
		got := sarifLevel(tc.sev)
		if got != tc.want {
			t.Errorf("sarifLevel(%v) = %q, want %q", tc.sev, got, tc.want)
		}
	}
}
