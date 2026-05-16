# Spec: Active-Window Focus Detection

## Overview

`internal/focus` is responsible for determining whether the target game window is currently in focus. The remapper loop calls the focus tracker before forwarding each key event; if the game is not in focus, the event is passed through without remapping.

This package is **not yet implemented**.

---

## Goals

- Detect when a specific window (identified by its title or process name) gains or loses focus.
- Support both X11 and Wayland compositors without requiring root or X11 libraries linked at compile time.
- Operate with low latency (< 50 ms to detect a focus change).
- Fall back gracefully when the display server cannot be detected.

---

## API

```go
// Tracker reports whether the target window currently has focus.
type Tracker interface {
    // IsFocused returns true if the window whose title contains targetTitle
    // (case-insensitive substring match) is the currently active window.
    // It must be safe to call concurrently.
    IsFocused(targetTitle string) (bool, error)

    // Close releases any resources held by the tracker (e.g. dbus connections,
    // open pipes). After Close, IsFocused must return an error.
    Close() error
}

// New returns a Tracker appropriate for the current desktop environment.
// It detects the compositor by inspecting environment variables:
//   WAYLAND_DISPLAY → try WaylandTracker implementations in order (sway, hyprland, generic)
//   DISPLAY         → try X11Tracker
//   neither set     → return ErrNoDisplay
func New() (Tracker, error)
```

---

## Compositor Detection

`New()` inspects environment variables to choose the appropriate backend:

| Condition | Backend chosen |
|-----------|---------------|
| `WAYLAND_DISPLAY` is set and `SWAYSOCK` is set | `SwayTracker` |
| `WAYLAND_DISPLAY` is set and `HYPRLAND_INSTANCE_SIGNATURE` is set | `HyprlandTracker` |
| `WAYLAND_DISPLAY` is set (generic) | `WlrootsFocusTracker` (via `wlr-foreign-toplevel-management` protocol, if available) or fall back to polling `wl-rootsctl` if present |
| `DISPLAY` is set | `X11Tracker` |
| Neither set | Return `ErrNoDisplay` |

---

## X11 Tracker (`X11Tracker`)

Uses `xdotool` and/or `xprop` (called as external processes) to detect the active window name. No X11 Go bindings are linked.

### Polling Loop

```
every 100 ms:
    active_window_id = xdotool getactivewindow
    window_name      = xdotool getwindowname <id>
    update internal focused state
```

Alternatively, subscribe to `_NET_ACTIVE_WINDOW` property changes on the root window using `xprop -spy -root _NET_ACTIVE_WINDOW` to avoid polling.

### `IsFocused(targetTitle string) (bool, error)`

Returns `true` if the cached active window name contains `targetTitle` (case-insensitive).

---

## Sway Tracker (`SwayTracker`)

Uses `swaymsg -t subscribe -m '["window"]'` to subscribe to window focus events over the Sway IPC socket. Parses the JSON output to maintain the current focused window title.

```json
// Example sway window focus event
{
  "change": "focus",
  "container": {
    "name": "Hearts of Iron IV",
    ...
  }
}
```

### `IsFocused(targetTitle string) (bool, error)`

Returns `true` if the most recently seen focused container's `name` contains `targetTitle` (case-insensitive).

---

## Hyprland Tracker (`HyprlandTracker`)

Uses `hyprctl activewindow -j` (polled every 100 ms, or via the Hyprland event socket at `$XDG_RUNTIME_DIR/hypr/$HYPRLAND_INSTANCE_SIGNATURE/.socket2.sock`) to get the active window title.

---

## Configuration

The focus target is specified in the config file:

```yaml
focus:
  window_title: "Hearts of Iron IV"    # case-insensitive substring match
```

If `focus.window_title` is absent or empty, focus tracking is **disabled** and the remapper runs unconditionally (remapping is always active).

### Config Struct

```go
type FocusConfig struct {
    WindowTitle string `mapstructure:"window_title"`
}

// Add to AppConfig:
type AppConfig struct {
    LogLevel string      `mapstructure:"log_level"`
    Focus    FocusConfig `mapstructure:"focus"`
    KeyMaps  []Keymap    `mapstructure:"keymaps"`
}
```

---

## Behaviours

### FOC-01 — Focus tracking disabled when window_title is empty
**Given** `focus.window_title` is not set in config  
**When** the remapper initialises  
**Then** no focus tracker is created and all key events are remapped unconditionally

### FOC-02 — Remapping is active when the target window is focused
**Given** focus tracking is enabled with `window_title: "Hearts of Iron IV"`  
**And** the active window is "Hearts of Iron IV"  
**When** `IsFocused("Hearts of Iron IV")` is called  
**Then** `true` is returned

### FOC-03 — Remapping is suspended when the target window loses focus
**Given** focus tracking is enabled  
**And** the user switches to a different window (e.g. a browser)  
**When** `IsFocused("Hearts of Iron IV")` is called  
**Then** `false` is returned

### FOC-04 — Focus change is detected within 150 ms
**Given** a running tracker  
**When** the target window gains or loses focus  
**Then** `IsFocused` returns the updated state within 150 ms

### FOC-05 — X11 backend is selected when DISPLAY is set and WAYLAND_DISPLAY is not
**Given** `DISPLAY=:0` is set in the environment and `WAYLAND_DISPLAY` is unset  
**When** `New()` is called  
**Then** an `X11Tracker` is returned

### FOC-06 — Sway backend is selected when SWAYSOCK is set
**Given** `WAYLAND_DISPLAY` and `SWAYSOCK` are both set  
**When** `New()` is called  
**Then** a `SwayTracker` is returned

### FOC-07 — ErrNoDisplay is returned when no display server is detected
**Given** neither `DISPLAY` nor `WAYLAND_DISPLAY` is set  
**When** `New()` is called  
**Then** an error wrapping `ErrNoDisplay` is returned

### FOC-08 — Title match is case-insensitive
**Given** the active window title is `"hearts of iron iv"`  
**When** `IsFocused("Hearts of Iron IV")` is called  
**Then** `true` is returned

### FOC-09 — Tracker cleans up resources on Close
**Given** a running tracker  
**When** `Close()` is called  
**Then** all background goroutines and external process handles are released; subsequent calls to `IsFocused` return an error
