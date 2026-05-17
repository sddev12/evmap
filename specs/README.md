# evmap — Specification Index

This folder contains the functional and behavioural specifications for `evmap`, a Linux key-remapper daemon. Development follows a **spec-driven** approach: each spec document defines the expected behaviour of one package or subsystem, and implementation is written to satisfy those specs.

## Motivation

Games such as _Hearts of Iron 4_ and _Medieval II: Total War_ hard-code the arrow keys for map scrolling and provide no in-game key-binding UI. `evmap` intercepts raw keyboard input at the OS level, translates configured key presses (e.g. `up → w`), and emits the translated events through a virtual keyboard — so the game sees `w` pressed whenever the user presses `↑`, without any modification to the game itself.

Remapping is active **only while the target game window is in focus**, so normal typing behaviour is preserved in every other application.

## Spec Documents

| File | Subsystem | Status |
|------|-----------|--------|
| [config.md](config.md) | Config loading & validation | Complete |
| [input.md](input.md) | evdev device discovery & event reading | Complete |
| [output.md](output.md) | uinput virtual keyboard & event emission | Complete |
| [focus.md](focus.md) | Active-window detection & focus tracking | Complete |
| [remapper.md](remapper.md) | Core remapping loop | Complete |
| [cli.md](cli.md) | CLI behaviour & flags | Complete |
| [ci.md](ci.md) | CI/CD workflows & release automation | Draft |

## Package Layout

```
main.go                  entry point → cmd.Execute()
cmd/
  root.go                Cobra root command; Viper config init; launches remapper
  root_test.go           CLI integration tests
internal/
  config/
    config.go            AppConfig + Keymap structs (mapstructure tags)
    config_test.go       Config validation tests
  input/
    input.go             evdev Device: open, grab, read events
    input_test.go        Input device tests
  output/
    output.go            uinput Device: create virtual keyboard, emit events
    output_test.go       uinput tests
  focus/
    focus.go             Tracker interface + compositor detection
    focus_test.go        Focus tracking tests
    sway.go              Sway compositor backend
    hyprland.go          Hyprland compositor backend
    kwin.go              KWin compositor backend
  remapper/
    remapper.go          Core remapping loop with hot-reload
    remapper_test.go     Remapper tests
```

## Development Conventions

- Each spec must have a **Behaviour** section written as testable scenarios:
  `Given … When … Then …`
- Implementation targets the spec; tests codify the spec.
- If implementation diverges from the spec, update the spec first (and document why) before merging.
- Use `log/slog` for structured logging; honour `log_level` from config.
- Errors are wrapped with `fmt.Errorf("…: %w", err)` and propagated; `log.Fatal` is only permitted in `main` or CLI entry points.
