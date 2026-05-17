/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"evmap/internal/config"
	"evmap/internal/focus"
	"evmap/internal/input"
	"evmap/internal/output"
	"evmap/internal/remapper"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sys/unix"
)

var (
	cfgFile      string
	logLevelFlag string
	deviceFlag   string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "evmap",
	Short: "Linux key remapper for gamers",
	Long:  `evmap intercepts keyboard input at the OS level and remaps configured keys to new keys. Remapping is only active when the target window is in focus.`,

	Run: handleRoot,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.evmap.yaml)")
	rootCmd.PersistentFlags().StringVar(&logLevelFlag, "log-level", "", "override log level (DEBUG, INFO, WARN, ERROR)")
	rootCmd.PersistentFlags().StringVar(&deviceFlag, "device", "", "path to specific /dev/input/eventN device")
}

func handleRoot(cmd *cobra.Command, args []string) {
	// Step 1-6: Load and validate config
	cfg, err := loadAndValidateConfig(cfgFile, logLevelFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	// Step 7: Initialize slog with configured log level
	initLogger(cfg.LogLevel)

	// Step 8: Discover or open input device
	devicePath, err := discoverInputDevice(deviceFlag)
	if err != nil {
		slog.Error("failed to find input device", "error", err)
		os.Exit(1)
	}

	inputDev, err := input.Open(devicePath)
	if err != nil {
		slog.Error("failed to open input device", "error", err)
		os.Exit(1)
	}
	slog.Info("input device opened", "path", devicePath)

	// Step 9: Open output device (uinput)
	outputDev, err := output.Open()
	if err != nil {
		slog.Error("failed to open uinput device", "error", err)
		inputDev.Close()
		os.Exit(1)
	}
	slog.Info("uinput device opened")

	// Step 10: Initialize focus tracker if configured
	var focusTracker focus.Tracker
	if cfg.Focus.WindowTitle != "" {
		focusTracker, err = focus.New()
		if err != nil {
			slog.Error("failed to initialize focus tracker", "error", err)
			outputDev.Close()
			inputDev.Close()
			os.Exit(1)
		}
		slog.Info("focus tracker initialized", "target", cfg.Focus.WindowTitle)
	}

	// Step 11: Construct Remapper
	remap, err := remapper.New(inputDev, outputDev, focusTracker, cfg.Focus.WindowTitle, cfg.KeyMaps)
	if err != nil {
		slog.Error("failed to create remapper", "error", err)
		if focusTracker != nil {
			focusTracker.Close()
		}
		outputDev.Close()
		inputDev.Close()
		os.Exit(1)
	}

	// Step 12-13: Register signal handlers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Create reloader wrapper for SIGHUP
	reloader := &remapperReloader{
		remapper: remap,
		cfgPath:  viper.ConfigFileUsed(),
	}

	go handleSignals(ctx, cancel, reloader, signalCh)

	// Step 14: Run remapper
	if err := remap.Run(ctx); err != nil {
		slog.Error("remapper error", "error", err)
		os.Exit(1)
	}

	// Step 15: Clean shutdown
	if focusTracker != nil {
		focusTracker.Close()
	}
	slog.Info("evmap stopped")
}

// loadConfig loads the configuration file and returns an error with a helpful message if not found.
func loadConfig(configPath string) error {
	viper.SetConfigName(".evmap")
	viper.SetConfigType("yaml")

	if configPath != "" {
		// Use explicit config path from --config flag
		viper.SetConfigFile(configPath)
	} else {
		// Search in current directory and home directory
		viper.AddConfigPath(".")
		homeDir, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(homeDir)
		}
	}

	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return fmt.Errorf(`Error: config file not found.
Create $HOME/.evmap.yaml or pass --config /path/to/file.yaml

Example config:
  log_level: INFO
  keymaps:
    - from: up
      to: w
    - from: down
      to: s`)
		}
		return fmt.Errorf("error reading config: %w", err)
	}

	return nil
}

// loadAndValidateConfig loads config, unmarshals it, applies overrides, and validates.
func loadAndValidateConfig(configPath, logLevelOverride string) (*config.AppConfig, error) {
	// Steps 1-3: Parse flags, initialize Viper, read config file
	if err := loadConfig(configPath); err != nil {
		return nil, err
	}

	// Step 4: Unmarshal config into AppConfig
	var cfg config.AppConfig
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Step 5: Apply --log-level flag override if provided
	if logLevelOverride != "" {
		cfg.LogLevel = logLevelOverride
	}

	// Step 6: Validate AppConfig
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed:\n%w", err)
	}

	return &cfg, nil
}

// initLogger initializes the global slog logger with the specified level.
func initLogger(levelStr string) {
	var level slog.Level
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

// discoverInputDevice implements step 8: discover or select input device.
func discoverInputDevice(deviceFlagValue string) (string, error) {
	// Step 8a: If --device flag is set, use it directly
	if deviceFlagValue != "" {
		return deviceFlagValue, nil
	}

	// Step 8b: Run Discover()
	devices, err := input.Discover()
	if err != nil {
		return "", fmt.Errorf("device discovery failed: %w", err)
	}

	// Step 8c: If no device found, exit with helpful message
	if len(devices) == 0 {
		return "", fmt.Errorf("no keyboard devices found — ensure you are in the 'input' group: sudo usermod -aG input $USER")
	}

	// If exactly one device, use it
	if len(devices) == 1 {
		return devices[0], nil
	}

	// Multiple devices found: prompt if TTY, otherwise pick first with warning
	return selectInputDevice(deviceFlagValue, devices)
}

// selectInputDevice handles multiple device selection.
func selectInputDevice(deviceFlagValue string, devices []string) (string, error) {
	// If --device flag is set, always return it (bypasses discovery)
	if deviceFlagValue != "" {
		return deviceFlagValue, nil
	}

	// Check if running with a TTY (interactive)
	if isatty(os.Stdin.Fd()) {
		// Interactive: prompt user to choose
		fmt.Fprintf(os.Stderr, "Multiple keyboard devices found:\n")
		for i, dev := range devices {
			fmt.Fprintf(os.Stderr, "  %d: %s\n", i+1, dev)
		}
		fmt.Fprintf(os.Stderr, "Select device [1-%d]: ", len(devices))

		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			choice, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
			if err != nil || choice < 1 || choice > len(devices) {
				return "", fmt.Errorf("invalid selection")
			}
			return devices[choice-1], nil
		}
		return "", fmt.Errorf("failed to read selection")
	}

	// Non-interactive: select first device and log warning
	slog.Warn("multiple devices found; selecting first", "devices", devices, "selected", devices[0])
	return devices[0], nil
}

// isatty checks if the given file descriptor is connected to a terminal.
func isatty(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), unix.TCGETS)
	return err == nil
}

// handleSignals listens for SIGINT, SIGTERM, and SIGHUP and handles them appropriately.
func handleSignals(ctx context.Context, cancel context.CancelFunc, reloader Reloader, signalCh <-chan os.Signal) {
	for {
		select {
		case sig := <-signalCh:
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				slog.Info("received shutdown signal", "signal", sig)
				cancel()
				return
			case syscall.SIGHUP:
				slog.Info("received SIGHUP, reloading config")
				if reloader != nil {
					if err := reloader.Reload(); err != nil {
						slog.Warn("config reload failed", "error", err)
					}
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// Reloader is the interface for hot-reloading configuration.
type Reloader interface {
	Reload() error
}

// remapperReloader wraps a remapper and implements config hot-reload.
type remapperReloader struct {
	remapper *remapper.Remapper
	cfgPath  string
}

func (r *remapperReloader) Reload() error {
	// Re-read config file
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var cfg config.AppConfig
	if err := viper.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	return r.remapper.Reload(cfg.KeyMaps)
}
