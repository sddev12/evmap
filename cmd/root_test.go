package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// TestCLI01_ConfigFileNotFoundExitsWithHelpfulMessage codifies CLI-01.
func TestCLI01_ConfigFileNotFoundExitsWithHelpfulMessage(t *testing.T) {
	// Use a temporary directory with no config file
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Reset viper state
	viper.Reset()

	err := loadConfig("")
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "config file not found") {
		t.Errorf("expected 'config file not found' in error message, got %q", errMsg)
	}
	if !strings.Contains(errMsg, ".evmap.yaml") {
		t.Errorf("expected '.evmap.yaml' mentioned in error message, got %q", errMsg)
	}
	if !strings.Contains(errMsg, "Example config:") {
		t.Errorf("expected 'Example config:' in error message, got %q", errMsg)
	}
}

// TestCLI02_ValidationErrorsExitWithCode1 codifies CLI-02.
func TestCLI02_ValidationErrorsExitWithCode1(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".evmap.yaml")

	// Write invalid config (missing required keymaps)
	cfgContent := `log_level: INFO
keymaps: []
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	viper.Reset()
	viper.SetConfigFile(cfgPath)

	cfg, err := loadAndValidateConfig(cfgPath, "")
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if cfg != nil {
		t.Fatal("expected nil config on validation error")
	}

	if !strings.Contains(err.Error(), "at least one keymap entry is required") {
		t.Errorf("expected validation error message, got %q", err.Error())
	}
}

// TestCLI03_ConfigFlagOverridesDefaultSearchPath codifies CLI-03.
func TestCLI03_ConfigFlagOverridesDefaultSearchPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create config in custom location
	customPath := filepath.Join(tmpDir, "custom.yaml")
	cfgContent := `log_level: INFO
keymaps:
  - from: up
    to: w
`
	if err := os.WriteFile(customPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Create different config in home location (should be ignored)
	homePath := filepath.Join(tmpDir, ".evmap.yaml")
	wrongContent := `log_level: DEBUG
keymaps:
  - from: down
    to: s
`
	if err := os.WriteFile(homePath, []byte(wrongContent), 0o644); err != nil {
		t.Fatalf("failed to write home config: %v", err)
	}

	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	viper.Reset()

	// Load with custom path
	cfg, err := loadAndValidateConfig(customPath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have loaded the custom config (up->w), not the home config (down->s)
	if len(cfg.KeyMaps) != 1 || cfg.KeyMaps[0].From != "up" {
		t.Errorf("expected keymap from custom config, got %+v", cfg.KeyMaps)
	}
}

// TestCLI04_LogLevelFlagOverridesConfig codifies CLI-04.
func TestCLI04_LogLevelFlagOverridesConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".evmap.yaml")

	cfgContent := `log_level: INFO
keymaps:
  - from: up
    to: w
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	viper.Reset()

	cfg, err := loadAndValidateConfig(cfgPath, "DEBUG")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LogLevel != "DEBUG" {
		t.Errorf("expected log level DEBUG (from flag override), got %q", cfg.LogLevel)
	}
}

// TestCLI05_SIGINTTriggersGracefulShutdown codifies CLI-05.
func TestCLI05_SIGINTTriggersGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)

	// Start signal handler
	go func() {
		time.Sleep(50 * time.Millisecond)
		signalCh <- syscall.SIGINT
	}()

	shutdownCh := make(chan struct{})
	go func() {
		handleSignals(ctx, cancel, nil, signalCh)
		close(shutdownCh)
	}()

	// Wait for shutdown
	select {
	case <-shutdownCh:
		// Success: signal handler returned
	case <-time.After(1 * time.Second):
		t.Fatal("signal handler did not trigger shutdown within timeout")
	}

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Success
	default:
		t.Error("context should have been cancelled after SIGINT")
	}
}

// TestCLI06_SIGTERMTriggersGracefulShutdown codifies CLI-06.
func TestCLI06_SIGTERMTriggersGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)

	go func() {
		time.Sleep(50 * time.Millisecond)
		signalCh <- syscall.SIGTERM
	}()

	shutdownCh := make(chan struct{})
	go func() {
		handleSignals(ctx, cancel, nil, signalCh)
		close(shutdownCh)
	}()

	select {
	case <-shutdownCh:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("signal handler did not trigger shutdown within timeout")
	}

	select {
	case <-ctx.Done():
		// Success
	default:
		t.Error("context should have been cancelled after SIGTERM")
	}
}

// TestCLI07_SIGHUPReloadsConfig codifies CLI-07.
func TestCLI07_SIGHUPReloadsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".evmap.yaml")

	// Write initial config
	cfgContent := `log_level: INFO
keymaps:
  - from: up
    to: w
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	viper.Reset()
	viper.SetConfigFile(cfgPath)

	// Mock remapper for reload testing
	reloadCount := 0
	mockReloader := &mockRemapperReloader{
		reloadFunc: func() error {
			reloadCount++
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)

	// Update config and send SIGHUP
	go func() {
		time.Sleep(50 * time.Millisecond)

		// Update config file
		newContent := `log_level: INFO
keymaps:
  - from: up
    to: s
`
		os.WriteFile(cfgPath, []byte(newContent), 0o644)

		signalCh <- syscall.SIGHUP
		time.Sleep(100 * time.Millisecond)
		cancel() // Stop the signal handler
	}()

	handleSignals(ctx, cancel, mockReloader, signalCh)

	if reloadCount != 1 {
		t.Errorf("expected reload to be called once, got %d times", reloadCount)
	}
}

// TestCLI08_DeviceFlagBypassesDiscovery codifies CLI-08.
func TestCLI08_DeviceFlagBypassesDiscovery(t *testing.T) {
	// This test verifies the device flag logic
	// When --device is set, discovery should be skipped

	devicePath := "/dev/input/event4"

	// selectInputDevice should return the specified device directly
	selected, err := selectInputDevice(devicePath, []string{})
	if err != nil {
		t.Fatalf("unexpected error when device flag is set: %v", err)
	}

	if selected != devicePath {
		t.Errorf("expected device path %q, got %q", devicePath, selected)
	}

	// Verify discovery list is ignored when device flag is set
	discoveryList := []string{"/dev/input/event0", "/dev/input/event1"}
	selected, err = selectInputDevice(devicePath, discoveryList)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if selected != devicePath {
		t.Errorf("expected device flag to override discovery, got %q", selected)
	}
}

// mockRemapperReloader is a test double for config hot-reload testing.
type mockRemapperReloader struct {
	reloadFunc func() error
}

func (m *mockRemapperReloader) Reload() error {
	if m.reloadFunc != nil {
		return m.reloadFunc()
	}
	return nil
}
