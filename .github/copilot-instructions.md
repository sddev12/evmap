# evmap — Copilot Instructions

## Project Overview

`evmap` is a Linux key remapper daemon written in Go. It intercepts raw keyboard events at the OS level via `/dev/input`, optionally translates them according to a user-defined keymap, and re-emits them through a `uinput` virtual keyboard. Remapping is only active when a configured target window is in focus, so normal typing is unaffected in all other applications.

The primary use case is games that hard-code keys with no in-game rebinding UI (e.g. Paradox titles such as Hearts of Iron 4, or older Total War games), where the user wants to use WASD for map scrolling instead of the arrow keys.

## Spec-Driven Development

**This project is built spec-first.** All functional requirements, API contracts, and expected behaviours live in the `specs/` folder. Before implementing or modifying any subsystem, read the relevant spec. If implementation needs to diverge from the spec, update the spec first (with justification) before merging code.

| Spec | Subsystem |
|------|-----------|
| [`specs/config.md`](../specs/config.md) | Config loading, validation, key-name registry |
| [`specs/input.md`](../specs/input.md) | evdev device discovery, exclusive grab, event reading |
| [`specs/output.md`](../specs/output.md) | uinput virtual keyboard creation and event emission |
| [`specs/focus.md`](../specs/focus.md) | Active-window detection (X11, Sway, Hyprland) |
| [`specs/remapper.md`](../specs/remapper.md) | Core remapping loop, keymap hot-reload, shutdown |
| [`specs/cli.md`](../specs/cli.md) | CLI flags, startup sequence, signal handling, exit codes |

Each spec contains **Behaviour** scenarios written as `Given / When / Then` statements. Tests must codify these scenarios. Do not add behaviour that is not described in a spec without first adding it to the spec.

## Config Format

```yaml
log_level: DEBUG   # DEBUG | INFO | WARN | ERROR (default: INFO)

focus:
  window_title: "Hearts of Iron IV"   # optional; omit to remap unconditionally

keymaps:
  - from: up       # key to intercept
    to: w          # key to emit instead
  - from: down
    to: s
  - from: left
    to: a
  - from: right
    to: d
```

Key names use human-readable lower-case identifiers (`up`, `down`, `left`, `right`, `enter`, `space`, `a`–`z`, `0`–`9`, `f1`–`f12`, etc.). The full list of supported names is defined in `specs/config.md`. The config is loaded from `$HOME/.evmap.yaml` or the current working directory by default; override with `--config`.

## Tech Stack

- **Language**: Go (module `evmap`)
- **CLI framework**: [Cobra](https://github.com/spf13/cobra) + [Viper](https://github.com/spf13/viper) for config parsing
- **Key interception**: `/dev/input` event devices via `golang.org/x/sys/unix`. Do **not** use X11/Xlib or Wayland protocol libraries for input interception.
- **Focus detection**: External processes only — `xdotool`/`xprop` on X11; `swaymsg` on Sway; `hyprctl` on Hyprland. No compositor protocol libraries linked at compile time.
- **Key emission**: `uinput` virtual device via `golang.org/x/sys/unix`.

## Project Structure

```
main.go                     # entry point — calls cmd.Execute()
cmd/
  root.go                   # root Cobra command, Viper init, startup sequence
internal/
  config/
    config.go               # AppConfig, Keymap structs; Validate(); key-name registry
  input/
    input.go                # evdev Device: Discover, Open, ReadEvents, Close
  output/
    output.go               # uinput Device: Open, WriteEvent, WriteSyn, Close
  focus/                    # Tracker interface + X11/Sway/Hyprland backends
  remapper/                 # Remapper: event loop, keymap compilation, hot-reload
specs/
  README.md                 # spec index and conventions
  config.md / input.md / output.md / focus.md / remapper.md / cli.md
```

## Coding Conventions

- Follow standard Go project layout; keep packages small and single-purpose.
- All packages live under `internal/` unless they expose a public CLI interface.
- Struct fields use `mapstructure` tags so Viper can unmarshal YAML directly into typed structs.
- Use `log/slog` for structured logging; honour the `log_level` field from config.
- Wrap errors with `fmt.Errorf("…: %w", err)` and propagate upward. `log.Fatal` is only permitted in `main` or CLI entry points.
- Use `context.Context` for cancellation and graceful shutdown.
- Write one `_test.go` file per non-trivial package; test scenarios map 1-to-1 to the `Given/When/Then` behaviours in the spec.

## Key Behaviour Requirements

Full details are in the specs. In summary:

1. **Intercept-only when focused** — events are passed through unchanged when the target window is not in focus. See `specs/focus.md` and `specs/remapper.md`.
2. **Exclusive grab** — `EVIOCGRAB` prevents the original event reaching other processes. See `specs/input.md`.
3. **Graceful shutdown** — `SIGINT`/`SIGTERM` releases the grab and destroys the uinput device before exit. See `specs/cli.md`.
4. **No root required** — users add themselves to the `input` and `uinput` groups. Helpful error messages guide them if permissions are missing.
5. **Config hot-reload** — `SIGHUP` reloads keymaps without restarting. See `specs/remapper.md` and `specs/cli.md`.

## Out of Scope

- Windows or macOS support (Linux-only).
- GUI configuration tool.
- Remapping mouse buttons (keyboard keys only, for now).
