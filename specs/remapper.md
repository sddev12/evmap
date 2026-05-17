# Spec: Core Remapping Loop

## Overview

`internal/remapper` ties together the input device, output device, focus tracker, and keymap to form the main event-processing loop. It reads raw key events from the input device, decides whether to remap them (based on focus state and the configured keymap), and writes the translated events to the virtual keyboard.

This package is **not yet implemented**.

---

## Responsibilities

- Build a lookup map from the loaded `[]config.Keymap` at startup.
- Start and manage the `input.Device` read loop.
- On each key event:
  - If a focus tracker is configured: skip remapping (pass the event through as-is) when the target window is not focused.
  - If the event's key code is in the keymap: replace the code with the mapped code.
  - Write the (possibly remapped) event to the `output.Device`.
- Emit a `SYN_REPORT` after every key event write.
- Handle graceful shutdown when the context is cancelled.
- Support config hot-reload on `SIGHUP`.

---

## API

```go
// Remapper is the top-level remapping engine.
type Remapper struct { /* unexported fields */ }

// New creates a Remapper from the given components.
// keymaps is the validated list from config; it is compiled into an internal
// lookup table (map[uint16]uint16) keyed by input key code.
func New(
    in     InputDevice,
    out    OutputDevice,
    focus  focus.Tracker,   // may be nil (no focus filtering)
    keymaps []config.Keymap,
) (*Remapper, error)

// Run starts the remapping loop and blocks until ctx is cancelled.
// It returns any fatal error that caused the loop to exit prematurely.
func (r *Remapper) Run(ctx context.Context) error

// Reload replaces the active keymap with the provided one without restarting
// the loop. Safe to call from a signal handler goroutine.
func (r *Remapper) Reload(keymaps []config.Keymap) error
```

### Interface Types (for testability)

```go
type InputDevice interface {
    ReadEvents(ctx context.Context, ch chan<- input.InputEvent)
    Close() error
}

type OutputDevice interface {
    WriteEvent(timeSec, timeUsec int64, evType, code uint16, value int32) error
    WriteSyn(timeSec, timeUsec int64) error
    Close() error
}
```

---

## Keymap Compilation

At construction and on reload, `[]config.Keymap` is compiled into a `map[uint16]uint16` where:
- Key = Linux key code for `Keymap.From`
- Value = Linux key code for `Keymap.To`

Key name → code resolution uses the registry defined in [config.md](config.md#key-name-registry).

If compilation fails (unknown key name), `New` / `Reload` returns an error without modifying the active keymap.

---

## Event Processing Loop

```
start ReadEvents goroutine → eventCh

for {
    select {
    case ev <- eventCh:
        process(ev)
    case <-ctx.Done():
        return ctx.Err()
    }
}

process(ev):
    if ev.Type == EvSyn:
        // Forward SYN events as-is to preserve original event batching.
        // Applications expect MSC_SCAN and KEY_* to be in the same batch
        // (sharing the same timestamp), so we forward the original SYN
        // rather than generating new ones.
        out.WriteSyn(ev.TimeSec, ev.TimeUsec)
        return

    if ev.Type != EvKey:
        // Forward non-key events (e.g. EV_MSC) with original timestamp.
        // Some applications require MSC_SCAN events alongside key events.
        out.WriteEvent(ev.TimeSec, ev.TimeUsec, ev.Type, ev.Code, ev.Value)
        return

    if focusTracker != nil && !focusTracker.IsFocused(targetTitle):
        // Game not in focus — emit original event unchanged.
        // Do not call WriteSyn here; the original SYN will arrive separately.
        out.WriteEvent(ev.TimeSec, ev.TimeUsec, ev.Type, ev.Code, ev.Value)
        return

    // Apply keymap
    outCode := ev.Code
    if mapped, ok := keymap[ev.Code]; ok:
        outCode = mapped
    }
    // Write remapped (or unmapped) key event with original timestamp.
    // Do not call WriteSyn here; the original SYN will arrive separately.
    out.WriteEvent(ev.TimeSec, ev.TimeUsec, ev.Type, outCode, ev.Value)
```

---

## Graceful Shutdown

When `ctx` is cancelled:

1. The `ReadEvents` goroutine notices and returns (within ~50 ms).
2. The `Run` loop drains any remaining events from `eventCh` that were sent before cancellation.
3. `Run` calls `in.Close()` to release the exclusive grab.
4. `Run` calls `out.Close()` to destroy the virtual device.
5. `Run` returns `nil` (clean shutdown) or the context error if the shutdown was due to cancellation.

The caller (in `cmd/root.go`) is responsible for intercepting `SIGINT`/`SIGTERM` and cancelling the context.

---

## Config Hot-Reload (`SIGHUP`)

On receipt of `SIGHUP`, the remapper reloads its keymap from the config file:

1. Viper re-reads the config file.
2. The new `[]config.Keymap` is validated.
3. `r.Reload(newKeymaps)` is called; the new map replaces the old atomically (use `sync/atomic` or a `sync.RWMutex`).
4. If validation fails, the old keymap remains in effect and the error is logged.
5. The input/output devices and focus tracker are **not** restarted.

---

## Logging

All log output uses `log/slog` at the level configured in `AppConfig.LogLevel`.

| Event | Level | Message |
|-------|-------|---------|
| Remapper started | INFO | `"remapper started"` with `device` and `keymaps` fields |
| Key remapped | DEBUG | `"key remapped"` with `from` and `to` code fields |
| Key passed through (not in map) | DEBUG | `"key pass-through"` with `code` field |
| Key suppressed (not focused) | DEBUG | `"key suppressed (not focused)"` with `code` field |
| Config reloaded | INFO | `"config reloaded"` |
| Config reload failed | WARN | `"config reload failed"` with `err` field |
| Remapper stopped | INFO | `"remapper stopped"` |

---

## Behaviours

### RMP-01 — Configured key is remapped when in focus
**Given** keymap `{up → w}`, focus tracking enabled, game window is focused  
**When** a `KEY_UP` press event arrives  
**Then** `WriteEvent(evKey, KEY_W, 1)` and `WriteSyn()` are called on the output device

### RMP-02 — Unmapped key is passed through unchanged
**Given** keymap `{up → w}`  
**When** a `KEY_A` press event arrives (not in keymap)  
**Then** `WriteEvent(evKey, KEY_A, 1)` is called (original code preserved)

### RMP-03 — Remapping is suppressed when game is not focused
**Given** keymap `{up → w}`, game window is **not** focused  
**When** a `KEY_UP` press event arrives  
**Then** `WriteEvent(evKey, KEY_UP, 1)` is called (original code, no remapping)

### RMP-04 — Both press and release are remapped
**Given** keymap `{up → w}`  
**When** a `KEY_UP` press then a `KEY_UP` release arrive  
**Then** output sees `KEY_W` press then `KEY_W` release

### RMP-05 — Auto-repeat is remapped
**Given** keymap `{up → w}`  
**When** a `KEY_UP` auto-repeat event (value=2) arrives  
**Then** output sees `KEY_W` with value=2

### RMP-06 — Multiple keymaps all applied correctly
**Given** keymaps `{up→w, down→s, left→a, right→d}`  
**When** each of the four arrow keys is pressed  
**Then** output sees the corresponding WASD key for each

### RMP-07 — Shutdown releases input grab and destroys virtual device
**Given** a running remapper  
**When** the context is cancelled  
**Then** `in.Close()` and `out.Close()` are both called before `Run` returns

### RMP-08 — Reload replaces keymap without restart
**Given** a running remapper with keymap `{up → w}`  
**When** `Reload([{up → s}])` is called  
**Then** subsequent `KEY_UP` events are mapped to `KEY_S`

### RMP-09 — Failed reload leaves previous keymap active
**Given** a running remapper with keymap `{up → w}`  
**When** `Reload` is called with an invalid keymap (unknown key name)  
**Then** an error is returned and `KEY_UP` events still map to `KEY_W`

### RMP-10 — SYN events are forwarded from the input stream
**Given** a stream of events including `EV_SYN`/`SYN_REPORT`  
**When** the events are processed  
**Then** `WriteSyn()` is called for each `EV_SYN` event with its original timestamp preserved, and no additional `WriteSyn()` calls are made after key events
