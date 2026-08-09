package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".agentguard.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir
}

// TestLoadMissingReturnsEmpty guards the backward-compatible default: a scan
// target with no .agentguard.yaml yields an empty, no-op config (no error),
// so every corpus rule stays active.
func TestLoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load on missing file returned error: %v", err)
	}
	if !cfg.IsEmpty() {
		t.Fatalf("missing .agentguard.yaml should yield empty config, got %+v", cfg)
	}
}

// TestLoadEmptyFileReturnsEmpty: a present-but-empty config file is treated
// the same as a missing one.
func TestLoadEmptyFileReturnsEmpty(t *testing.T) {
	dir := writeConfig(t, "   \n\n  \n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load on empty file returned error: %v", err)
	}
	if !cfg.IsEmpty() {
		t.Fatalf("empty .agentguard.yaml should yield empty config, got %+v", cfg)
	}
}

// TestLoadParsesDisableAllowSeverity checks all three fields parse, and that
// rule IDs and severity values are normalised to lowercase so matching is
// case-insensitive.
func TestLoadParsesDisableAllowSeverity(t *testing.T) {
	dir := writeConfig(t, "disable:\n"+
		"  - AG004-Ignore-Previous-Instructions\n"+
		"  - ag005-conditional-on-ai-reader\n"+
		"allow:\n"+
		"  - AG001-address-coding-agent\n"+
		"severity:\n"+
		"  AG002-destructive-imperative: High\n"+
		"  AG001-address-coding-agent: low\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IsEmpty() {
		t.Fatal("expected non-empty config")
	}
	wantDisable := []string{"ag004-ignore-previous-instructions", "ag005-conditional-on-ai-reader"}
	if !reflect.DeepEqual(cfg.Disable, wantDisable) {
		t.Errorf("Disable = %v, want %v", cfg.Disable, wantDisable)
	}
	wantAllow := []string{"ag001-address-coding-agent"}
	if !reflect.DeepEqual(cfg.Allow, wantAllow) {
		t.Errorf("Allow = %v, want %v", cfg.Allow, wantAllow)
	}
	wantSev := map[string]string{
		"ag002-destructive-imperative": "high",
		"ag001-address-coding-agent":   "low",
	}
	if !reflect.DeepEqual(cfg.Severity, wantSev) {
		t.Errorf("Severity = %v, want %v", cfg.Severity, wantSev)
	}
}

// TestLoadMalformedReturnsError: a malformed YAML file fails the scan loudly
// so a typo does not silently weaken the gate (fail closed).
func TestLoadMalformedReturnsError(t *testing.T) {
	dir := writeConfig(t, "disable: [AG004, AG005\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error on malformed YAML, got nil")
	}
}

// TestLoadInvalidSeverityReturnsError: an unknown severity value fails the
// scan loudly rather than being silently ignored.
func TestLoadInvalidSeverityReturnsError(t *testing.T) {
	dir := writeConfig(t, "severity:\n  AG004-ignore-previous-instructions: hig\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error on invalid severity value, got nil")
	}
}

// TestLoadMedSeverityAliasAccepted: the "med" alias (accepted by
// detect.ParseSeverity and the CLI --severity flag) is also a valid override.
func TestLoadMedSeverityAliasAccepted(t *testing.T) {
	dir := writeConfig(t, "severity:\n  AG004-ignore-previous-instructions: med\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Severity["ag004-ignore-previous-instructions"]; got != "med" {
		t.Errorf("med alias not preserved: got %q", got)
	}
}
