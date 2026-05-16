# Spec: Config Loading & Validation

## Overview

`internal/config` is responsible for defining the typed configuration structs that Viper unmarshals YAML config into. It does **not** perform file I/O itself — that is delegated to Viper in `cmd/root.go`.

---

## Config File Format

The config file is YAML. The default search paths (in priority order) are:

1. Path passed to `--config` flag.
2. Current working directory (`.evmap.yaml`).
3. User home directory (`$HOME/.evmap.yaml`).

### Schema

```yaml
log_level: DEBUG          # optional; one of DEBUG | INFO | WARN | ERROR (default: INFO)

keymaps:                  # required; at least one entry
  - from: up              # key name to intercept (human-readable)
    to: w                 # key name to emit instead
```

---

## Structs

### `Keymap`

```go
type Keymap struct {
    From string `mapstructure:"from"`
    To   string `mapstructure:"to"`
}
```

### `AppConfig`

```go
type AppConfig struct {
    LogLevel string   `mapstructure:"log_level"`
    KeyMaps  []Keymap `mapstructure:"keymaps"`
}
```

---

## Validation

A `Validate() error` function must be added to `AppConfig`. It is called after Viper unmarshal and must enforce:

| Rule | Error message |
|------|---------------|
| `KeyMaps` must contain at least one entry | `"keymaps: at least one keymap entry is required"` |
| Each `Keymap.From` must be non-empty | `"keymaps[N].from: must not be empty"` |
| Each `Keymap.To` must be non-empty | `"keymaps[N].to: must not be empty"` |
| Each `Keymap.From` must be a recognised key name (resolved by the key-code registry) | `"keymaps[N].from: unknown key name \"X\""` |
| Each `Keymap.To` must be a recognised key name | `"keymaps[N].to: unknown key name \"X\""` |
| `LogLevel`, if non-empty, must be one of `DEBUG`, `INFO`, `WARN`, `ERROR` (case-insensitive) | `"log_level: invalid value \"X\"; must be one of DEBUG, INFO, WARN, ERROR"` |
| A key must not map to itself (`From == To`) | `"keymaps[N]: from and to must differ"` |

Validation errors for all failing rules are collected and returned together (not short-circuited after the first), wrapped in a single error value.

---

## Key Name Registry

A mapping from human-readable key names (lower-case strings) to Linux `KEY_*` codes is required. It must live in `internal/config` or a dedicated `internal/keycode` package.

### Required Key Names (minimum set)

The following names must be supported. The list can be extended in future without a breaking change.

| Name | Linux code | Notes |
|------|-----------|-------|
| `a`–`z` | `KEY_A`–`KEY_Z` | letter keys |
| `0`–`9` | `KEY_0`–`KEY_9` | number row |
| `up` | `KEY_UP` | arrow key |
| `down` | `KEY_DOWN` | arrow key |
| `left` | `KEY_LEFT` | arrow key |
| `right` | `KEY_RIGHT` | arrow key |
| `enter` | `KEY_ENTER` | |
| `space` | `KEY_SPACE` | |
| `tab` | `KEY_TAB` | |
| `esc` | `KEY_ESC` | |
| `backspace` | `KEY_BACKSPACE` | |
| `delete` | `KEY_DELETE` | |
| `home` | `KEY_HOME` | |
| `end` | `KEY_END` | |
| `pageup` | `KEY_PAGEUP` | |
| `pagedown` | `KEY_PAGEDOWN` | |
| `f1`–`f12` | `KEY_F1`–`KEY_F12` | function keys |
| `lshift`, `rshift` | `KEY_LEFTSHIFT`, `KEY_RIGHTSHIFT` | |
| `lctrl`, `rctrl` | `KEY_LEFTCTRL`, `KEY_RIGHTCTRL` | |
| `lalt`, `ralt` | `KEY_LEFTALT`, `KEY_RIGHTALT` | |
| `minus`, `equal`, `backslash` | `KEY_MINUS`, `KEY_EQUAL`, `KEY_BACKSLASH` | |
| `comma`, `dot`, `slash` | `KEY_COMMA`, `KEY_DOT`, `KEY_SLASH` | |
| `semicolon`, `apostrophe` | `KEY_SEMICOLON`, `KEY_APOSTROPHE` | |
| `leftbrace`, `rightbrace` | `KEY_LEFTBRACE`, `KEY_RIGHTBRACE` | |
| `grave` | `KEY_GRAVE` | backtick / tilde key |

Lookup is case-insensitive (normalise input to lower-case before lookup).

---

## Behaviours

### CFG-01 — Minimal valid config is accepted
**Given** a config file with `log_level: INFO` and one keymap `{from: up, to: w}`  
**When** `Validate()` is called  
**Then** no error is returned

### CFG-02 — Empty keymaps is rejected
**Given** a config file with an empty `keymaps` list (or the key absent)  
**When** `Validate()` is called  
**Then** an error is returned containing `"at least one keymap entry is required"`

### CFG-03 — Unknown key name in `from` is rejected
**Given** a keymap `{from: "xyz_unknown", to: w}`  
**When** `Validate()` is called  
**Then** an error is returned containing `"unknown key name"`

### CFG-04 — Unknown key name in `to` is rejected
**Given** a keymap `{from: up, to: "xyz_unknown"}`  
**When** `Validate()` is called  
**Then** an error is returned containing `"unknown key name"`

### CFG-05 — Self-mapping is rejected
**Given** a keymap `{from: up, to: up}`  
**When** `Validate()` is called  
**Then** an error is returned containing `"from and to must differ"`

### CFG-06 — Invalid log level is rejected
**Given** `log_level: VERBOSE`  
**When** `Validate()` is called  
**Then** an error is returned containing `"invalid value"`

### CFG-07 — All validation errors are reported together
**Given** a config with two invalid keymaps and an invalid log level  
**When** `Validate()` is called  
**Then** the returned error describes all three problems (not just the first)

### CFG-08 — Log level defaults to INFO
**Given** a config with no `log_level` field  
**When** `Validate()` is called and the config is used  
**Then** the effective log level is `INFO`

### CFG-09 — Key name lookup is case-insensitive
**Given** a keymap `{from: UP, to: W}`  
**When** `Validate()` is called  
**Then** no error is returned (names are normalised before lookup)
