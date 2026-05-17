# Spec: Input Device Discovery & Event Reading

## Overview

`internal/input` handles:

1. **Discovery** — finding the correct keyboard device under `/dev/input/`.
2. **Opening & grabbing** — exclusively claiming the device so raw events are not duplicated to other consumers.
3. **Reading** — streaming `InputEvent` values to the remapper loop via a channel.
4. **Closing** — releasing the grab and closing the file descriptor on shutdown.

The existing `input.go` already implements opening, grabbing, and reading. This spec adds the missing device-discovery behaviour and documents the complete contract for the package.

---

## Data Types

### `InputEvent`

```go
type InputEvent struct {
    TimeSec  int64
    TimeUsec int64
    Type     uint16
    Code     uint16
    Value    int32
}
```

Mirrors the kernel's `struct input_event` (24 bytes on 64-bit Linux). `Type` is one of `EvSyn`, `EvKey`, or `EvMsc`. For key events, `Value` is `1` (press), `0` (release), or `2` (auto-repeat).

### `DeviceInfo`

```go
type DeviceInfo struct {
    Path string // /dev/input/eventN
    Name string // Human-readable device name from EVIOCGNAME
}
```

Holds information about a discovered input device, including both its path and human-readable name.

### `Device`

```go
type Device struct { /* unexported fields */ }

func Open(path string) (*Device, error)
func (d *Device) Close() error
func (d *Device) ReadEvents(ctx context.Context, ch chan<- InputEvent)
```

---

## Device Discovery

### `Discover() ([]DeviceInfo, error)`

Returns information about all detected keyboard devices under `/dev/input/`. A device is considered a keyboard if:

- It is readable under `/dev/input/`.
- Its event-capability bitmask (obtained via `EVIOCGBIT(EV_KEY, ...)` ioctl) includes `KEY_A` (code 30). This heuristic reliably distinguishes keyboards from mice, joysticks, power buttons, etc.

The function must **not** open devices with exclusive grab; it only inspects capabilities.

#### Algorithm

1. Enumerate all files matching `/dev/input/event*`.
2. For each file, open it `O_RDONLY|O_NONBLOCK`.
3. Issue `EVIOCGBIT(EV_KEY, ...)` to read the key-capability bitmask.
4. Check whether bit `KEY_A` (30) is set.
5. If the device is a keyboard, issue `EVIOCGNAME` to retrieve its human-readable name.
6. Close the fd immediately after the checks.
7. Return a `DeviceInfo` struct containing both the path and name for each keyboard device found.

If no keyboard is found, return an empty slice and a nil error (the caller decides whether to treat that as fatal).

---

## Device Opening & Grabbing

`Open(path string) (*Device, error)` opens the evdev device at `path` and takes an exclusive grab (`EVIOCGRAB = 1`).

After `Open` returns:
- The kernel delivers **no further events from this device to any other process** (including the compositor/X server) until `Close` is called.
- This prevents the original key event from also reaching the game on top of the remapped one.

### Wait for Neutral Key State

**Before** issuing the exclusive grab, `Open` must wait until all keys are released. This prevents orphaned key releases from causing X11/Wayland auto-repeat issues.

The sequence is:

1. **Open** the device with `O_RDONLY|O_NONBLOCK`.
2. **Query key state** using `EVIOCGKEY` ioctl to retrieve a bitmask of currently pressed keys.
3. **Wait loop**: If any keys are pressed:
   - Sleep for 10ms
   - Query key state again
   - Repeat until all keys are released
4. **Propagation delay**: After all keys are released, sleep for 100ms to allow the release events to propagate to X11/Wayland before we grab the device. This ensures the compositor sees the releases and stops any auto-repeat mechanisms.
5. **Grab** the device with `EVIOCGRAB = 1`.
6. **Drain buffer**: Read and discard any stale events queued before the grab.

This approach (based on keyd's implementation) solves the phantom key repeat problem: when evmap is started by typing `./evmap` in a terminal, the Enter keypress is captured by the shell, but if we grab the device before the Enter key is released, the release event gets captured by evmap instead of reaching X11/Wayland. Without seeing the release, X11/Wayland's auto-repeat mechanism continues repeating the Enter key (~33ms intervals), causing dozens of empty lines in the terminal.

By waiting for neutral key state, we ensure the compositor sees all release events before we claim exclusive access.

### EVIOCGKEY Ioctl

The `EVIOCGKEY` ioctl (`_IOR('E', 0x18, char[size])`) retrieves a bitmask of currently pressed keys from the kernel. Each bit represents one key code (0-767 for `KEY_MAX`). Byte `N` contains bits for key codes `N*8` through `N*8+7`. If bit `K` is set, key code `K` is currently pressed according to the kernel's input subsystem state.

**Buffer Draining**: After the grab, `Open` drains any stale events from the kernel's input buffer. These events (such as unpaired key releases or MSC scan codes from the wait period) were queued before the grab. The device is opened with `O_NONBLOCK`, so draining simply reads until `EAGAIN`.

`Open` must fail if:
- The file does not exist or the caller lacks read permission.
- The `EVIOCGKEY` ioctl fails.
- The exclusive grab `ioctl` fails (e.g. another process already holds the grab).

---

## Event Reading

`ReadEvents(ctx context.Context, ch chan<- InputEvent)` runs an epoll-based read loop and is intended to be launched as a goroutine.

- It uses `epoll_wait` with a 50 ms timeout so that context cancellation is noticed within ~50 ms without a busy-wait loop.
- **All event types** (including `EV_MSC`, `EV_KEY`, and `EV_SYN`) are forwarded to `ch`. Applications may expect MSC_SCAN events alongside key events, so these must be preserved and emitted by the virtual keyboard.
- When `ctx` is cancelled the function returns, leaving `ch` open (the remapper owns the channel lifetime).
- I/O errors other than `EINTR` cause the function to return silently; the remapper detects the closed loop by observing that no further events arrive.

---

## Device Closing

`Close() error` must:
1. Release the exclusive grab (`EVIOCGRAB = 0`) so that the compositor regains access to the device.
2. Close the underlying file descriptor.
3. Return any error from step 2 (step 1 errors are best-effort and are ignored).

---

## Behaviours

### INP-01 — Discover returns only keyboard devices
**Given** the system has one keyboard (`/dev/input/event2`) and one mouse (`/dev/input/event3`)  
**When** `Discover()` is called  
**Then** only `/dev/input/event2` is returned

### INP-02 — Discover returns an empty slice when no keyboard is present
**Given** no keyboard devices exist under `/dev/input/`  
**When** `Discover()` is called  
**Then** the returned slice is empty and the error is nil

### INP-03 — Open succeeds on a valid path with accessible permissions
**Given** the calling process has read access to `/dev/input/eventN` and no other process holds a grab  
**When** `Open("/dev/input/eventN")` is called  
**Then** a non-nil `*Device` is returned and the error is nil

### INP-04 — Open fails when the grab is already held
**Given** another process holds an exclusive grab on `/dev/input/eventN`  
**When** `Open("/dev/input/eventN")` is called  
**Then** an error is returned containing `"grab"` and the fd is closed

### INP-05 — ReadEvents delivers key press and release events
**Given** a `*Device` opened on a test input device  
**When** a key-press and key-release are generated on that device  
**Then** both events arrive on `ch` with `Type == EvKey` and `Value` of `1` and `0` respectively

### INP-06 — ReadEvents forwards EV_MSC events
**Given** a `*Device` opened on a test input device  
**When** the device emits an `EV_MSC` scan-code event followed by a key press  
**Then** both the `EV_MSC` event and the key press arrive on `ch` in order

### INP-07 — ReadEvents stops when context is cancelled
**Given** a running `ReadEvents` goroutine  
**When** the context is cancelled  
**Then** the goroutine returns within 100 ms

### INP-08 — Close releases the grab
**Given** an open `*Device`  
**When** `Close()` is called  
**Then** another process can subsequently grab the same device without error

### INP-09 — Open waits for keys to be released before grabbing
**Given** a key is pressed on `/dev/input/eventN` when `Open` is called  
**When** `Open("/dev/input/eventN")` is called  
**Then** the function blocks until the key is released, then completes successfully

### INP-10 — Open drains stale events after grabbing
**Given** events are queued in the kernel buffer before the grab  
**When** `Open` completes successfully  
**Then** those events are discarded and do not appear on the `ReadEvents` channel

### INP-11 — Open adds propagation delay after keys released
**Given** a key is released just before `Open` attempts to grab  
**When** `Open` detects all keys are released  
**Then** it sleeps for 100ms before issuing `EVIOCGRAB` to allow X11/Wayland to process the release

### INP-12 — Discover returns device names
**Given** a keyboard device exists at `/dev/input/event2` with name "AT Translated Set 2 keyboard"  
**When** `Discover()` is called  
**Then** the returned `DeviceInfo` includes both `Path: "/dev/input/event2"` and `Name: "AT Translated Set 2 keyboard"`

---

## Permissions Note

`evmap` does not require root. The user must belong to the `input` group:

```bash
sudo usermod -aG input $USER
```

Log a clear error message if opening a device fails due to `EACCES` or `EPERM`:

```
input: permission denied on /dev/input/eventN — add yourself to the 'input' group
```
