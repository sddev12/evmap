package config

import (
	"strings"
	"testing"
)

// TestCFG01_MinimalValidConfig codifies CFG-01.
func TestCFG01_MinimalValidConfig(t *testing.T) {
	cfg := AppConfig{
		LogLevel: "INFO",
		KeyMaps: []Keymap{
			{From: "up", To: "w"},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}
}

// TestCFG02_EmptyKeymapsRejected codifies CFG-02.
func TestCFG02_EmptyKeymapsRejected(t *testing.T) {
	cfg := AppConfig{LogLevel: "INFO"}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "at least one keymap entry is required") {
		t.Fatalf("expected keymaps-empty message, got %q", err.Error())
	}
}

// TestCFG03_UnknownFromRejected codifies CFG-03.
func TestCFG03_UnknownFromRejected(t *testing.T) {
	cfg := AppConfig{
		LogLevel: "INFO",
		KeyMaps: []Keymap{
			{From: "xyz_unknown", To: "w"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown key name") {
		t.Fatalf("expected unknown-key message, got %q", err.Error())
	}
}

// TestCFG04_UnknownToRejected codifies CFG-04.
func TestCFG04_UnknownToRejected(t *testing.T) {
	cfg := AppConfig{
		LogLevel: "INFO",
		KeyMaps: []Keymap{
			{From: "up", To: "xyz_unknown"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown key name") {
		t.Fatalf("expected unknown-key message, got %q", err.Error())
	}
}

// TestCFG05_SelfMappingRejected codifies CFG-05.
func TestCFG05_SelfMappingRejected(t *testing.T) {
	cfg := AppConfig{
		LogLevel: "INFO",
		KeyMaps: []Keymap{
			{From: "up", To: "up"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "from and to must differ") {
		t.Fatalf("expected self-map message, got %q", err.Error())
	}
}

// TestCFG06_InvalidLogLevelRejected codifies CFG-06.
func TestCFG06_InvalidLogLevelRejected(t *testing.T) {
	cfg := AppConfig{
		LogLevel: "VERBOSE",
		KeyMaps: []Keymap{
			{From: "up", To: "w"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid value") {
		t.Fatalf("expected invalid-log-level message, got %q", err.Error())
	}
}

// TestCFG07_AllValidationErrorsReported codifies CFG-07.
func TestCFG07_AllValidationErrorsReported(t *testing.T) {
	cfg := AppConfig{
		LogLevel: "VERBOSE",
		KeyMaps: []Keymap{
			{From: "xyz_unknown", To: "w"},
			{From: "up", To: "abc_unknown"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	errText := err.Error()
	checks := []string{
		"keymaps[0].from: unknown key name",
		"keymaps[1].to: unknown key name",
		"log_level: invalid value",
	}
	for _, check := range checks {
		if !strings.Contains(errText, check) {
			t.Fatalf("expected aggregated error to contain %q, got %q", check, errText)
		}
	}
}

// TestCFG08_MissingLogLevelDefaultsToInfo codifies CFG-08.
func TestCFG08_MissingLogLevelDefaultsToInfo(t *testing.T) {
	cfg := AppConfig{
		KeyMaps: []Keymap{
			{From: "up", To: "w"},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}
	if cfg.LogLevel != "INFO" {
		t.Fatalf("expected log level default INFO, got %q", cfg.LogLevel)
	}
}

// TestCFG09_KeyLookupCaseInsensitive codifies CFG-09.
func TestCFG09_KeyLookupCaseInsensitive(t *testing.T) {
	cfg := AppConfig{
		LogLevel: "info",
		KeyMaps: []Keymap{
			{From: "UP", To: "W"},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no validation error, got %v", err)
	}

	upper, upperOK := LookupKeyCode("UP")
	lower, lowerOK := LookupKeyCode("up")
	if !upperOK || !lowerOK || upper != lower {
		t.Fatalf("expected case-insensitive key lookup, got upper=(%d,%t) lower=(%d,%t)", upper, upperOK, lower, lowerOK)
	}
}
