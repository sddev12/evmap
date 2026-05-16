# Spec: CLI Behaviour

## Overview

The CLI is built with [Cobra](https://github.com/spf13/cobra) and [Viper](https://github.com/spf13/viper). `cmd/root.go` defines the single root command (`evmap`) which, when invoked, loads config, builds all components, and runs the remapping loop until interrupted.

---

## Command: `evmap`

```
evmap [flags]
```

Starts the key remapper. Blocks until the user presses Ctrl-C or sends `SIGTERM`.

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--config` | string | `""` | Path to the config file. If empty, Viper searches `./` then `$HOME/` for `.evmap.yaml`. |
| `--log-level` | string | `""` | Override the `log_level` from config. One of `DEBUG`, `INFO`, `WARN`, `ERROR`. |
| `--device` | string | `""` | Path to a specific `/dev/input/eventN` device. If empty, device discovery is used automatically. |

---

## Startup Sequence

```
1. Parse flags (Cobra).
2. Initialise Viper: search paths ./  $HOME/  (then --config override if provided).
3. Read config file; fail with a user-friendly message if not found.
4. Unmarshal config into AppConfig.
5. Apply --log-level flag override if provided.
6. Validate AppConfig (see config spec); exit with code 1 on validation error.
7. Initialise slog with the configured log level.
8. Discover or open input device:
   a. If --device flag is set: open that path directly.
   b. Otherwise: run Discover(); if multiple devices found, prompt the user to choose
      or select the first one with a warning log.
   c. If no device found: exit with code 1 and a helpful message.
9. Open output device (uinput); exit with code 1 on failure with a helpful message.
10. Initialise focus tracker if focus.window_title is set in config.
11. Construct Remapper.
12. Register SIGINT / SIGTERM handler → cancels context.
13. Register SIGHUP handler → triggers config hot-reload.
14. Call remapper.Run(ctx); block until it returns.
15. Log "evmap stopped" and exit cleanly with code 0.
```

---

## Error Handling & Exit Codes

| Situation | Exit code | Output |
|-----------|-----------|--------|
| Config file not found | 1 | Human-readable message on stderr |
| Config validation error | 1 | All validation errors on stderr |
| No input device found | 1 | Helpful message suggesting group membership |
| Cannot open input device (permission) | 1 | Hint to join `input` group |
| Cannot open uinput (permission) | 1 | Hint to join `uinput` group and check udev rule |
| Focus tracker init failed (no display) | 1 | Suggest setting `DISPLAY` or `WAYLAND_DISPLAY` |
| Runtime error in remapping loop | 1 | Error logged via slog |
| Clean shutdown (SIGINT / SIGTERM) | 0 | `"evmap stopped"` logged at INFO |

---

## Config File Not Found: Example Message

```
Error: config file not found.
Create $HOME/.evmap.yaml or pass --config /path/to/file.yaml

Example config:
  log_level: INFO
  keymaps:
    - from: up
      to: w
    - from: down
      to: s
```

---

## Multiple Devices Found: Behaviour

If `Discover()` returns more than one keyboard path and `--device` is not set:

- If running with a TTY (interactive): print a numbered list and prompt the user to choose.
- If not a TTY (piped / scripted): select the first device, log a `WARN` message listing all found devices and stating which was chosen.

---

## SIGHUP Hot-Reload

On `SIGHUP`:
1. Viper re-reads the config file from the same path used at startup.
2. The new keymaps are validated.
3. On success: `remapper.Reload(newKeymaps)` is called; log `"config reloaded"` at INFO.
4. On failure: log `"config reload failed: <error>"` at WARN; previous keymaps remain active.

---

## Behaviours

### CLI-01 — Config file not found exits with code 1 and a helpful message
**Given** no `.evmap.yaml` exists in `./` or `$HOME/`  
**When** `evmap` is run without `--config`  
**Then** the process exits with code 1 and prints a message including `"config file not found"` and an example config

### CLI-02 — Validation errors are printed and the process exits with code 1
**Given** a config file with invalid keymaps  
**When** `evmap` is run  
**Then** all validation errors are printed to stderr and the process exits with code 1

### CLI-03 — `--config` overrides the default search path
**Given** a valid config file at `/tmp/test.yaml`  
**When** `evmap --config /tmp/test.yaml` is run  
**Then** Viper reads from `/tmp/test.yaml` and ignores `$HOME/.evmap.yaml`

### CLI-04 — `--log-level` overrides the config log level
**Given** a config file with `log_level: INFO`  
**And** the flag `--log-level DEBUG` is passed  
**Then** the effective log level is `DEBUG`

### CLI-05 — SIGINT triggers graceful shutdown with exit code 0
**Given** `evmap` is running  
**When** `SIGINT` is sent  
**Then** the process releases the input grab, destroys the uinput device, logs `"evmap stopped"`, and exits with code 0

### CLI-06 — SIGTERM triggers graceful shutdown with exit code 0
**Given** `evmap` is running  
**When** `SIGTERM` is sent  
**Then** the same shutdown sequence as CLI-05 occurs

### CLI-07 — SIGHUP reloads the config
**Given** `evmap` is running with keymap `{up → w}`  
**And** the config file is updated to map `{up → s}`  
**When** `SIGHUP` is sent  
**Then** subsequent `KEY_UP` events produce `KEY_S` output and `"config reloaded"` is logged

### CLI-08 — `--device` flag bypasses discovery
**Given** the flag `--device /dev/input/event4` is passed  
**When** `evmap` starts  
**Then** `/dev/input/event4` is opened directly without running `Discover()`
