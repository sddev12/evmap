# Spec: Virtual Keyboard Output (uinput)

## Overview

`internal/output` owns the uinput virtual keyboard device. It:

1. **Opens** `/dev/uinput`, registers event and key capabilities, and instantiates the virtual device.
2. **Emits** key events (press, release, auto-repeat) followed by a `SYN_REPORT` sync event.
3. **Closes** the virtual device cleanly on shutdown.

The existing `output.go` already implements all three steps. This spec documents the complete contract so that implementation and tests can be written against it.

---

## API

```go
type Device struct { /* unexported fields */ }

func Open() (*Device, error)
func (d *Device) WriteEvent(evType, code uint16, value int32) error
func (d *Device) WriteSyn() error
func (d *Device) Close() error
```

---

## Device Creation (`Open`)

`Open()` must:

1. Open `/dev/uinput` with `O_WRONLY|O_NONBLOCK`.
2. Register event type `EV_SYN` (0) via `UI_SET_EVBIT`.
3. Register event type `EV_KEY` (1) via `UI_SET_EVBIT`.
4. Register all key codes `0..KEY_MAX` (0x2FF) via `UI_SET_KEYBIT`.  
   Pre-registering the full range means the device can emit any key without needing reconfiguration when keymaps change.
5. Write a `uinput_user_dev` struct with:
   - `Name`: `"evmap virtual keyboard"` (null-padded to 80 bytes)
   - `BusType`: `0x03` (BUS_USB) — a common convention for synthetic devices
   - All other fields zero.
6. Issue `UI_DEV_CREATE` to instantiate the device.
7. Return the initialised `*Device`.

If any step fails, all previously acquired resources (fd) must be released before returning the error.

After `Open` returns successfully, the device appears in `/dev/input/` and is visible to udev and libinput.

---

## Writing Events (`WriteEvent` & `WriteSyn`)

### `WriteEvent(evType, code uint16, value int32) error`

Writes a single 24-byte `input_event` to the uinput fd using native byte order.

- `evType`: `evKey` (1) for key events.
- `code`: the Linux `KEY_*` code to emit.
- `value`: `1` for press, `0` for release, `2` for auto-repeat.
- The timestamp fields (`TimeSec`, `TimeUsec`) are set to the current wall-clock time.

### `WriteSyn() error`

Writes an `EV_SYN / SYN_REPORT` event (`type=0, code=0, value=0`). This must be called after every key press or release to flush the event batch to consumers.

#### Emit Sequence (for one remapped key press)

```
WriteEvent(evKey, remappedCode, 1)   // key press
WriteSyn()                           // flush
```

```
WriteEvent(evKey, remappedCode, 0)   // key release
WriteSyn()                           // flush
```

Auto-repeat (`value=2`) events from the input device are forwarded as-is with the same sequence.

---

## Device Teardown (`Close`)

`Close()` must:

1. Issue `UI_DEV_DESTROY` to unregister the virtual device from the kernel input layer. This removes it from `/dev/input/` and triggers a udev remove event.
2. Close the underlying file descriptor.
3. Return any error from step 2 (step 1 is best-effort; its error is ignored).

After `Close` returns, the device is no longer visible to other processes.

---

## Permissions Note

`evmap` does not require root. The user must belong to the `uinput` group:

```bash
sudo usermod -aG uinput $USER
```

Additionally, the udev rule must allow group write access to `/dev/uinput`. The typical rule file:

```
# /etc/udev/rules.d/99-uinput.rules
KERNEL=="uinput", GROUP="uinput", MODE="0660"
```

Log a clear error message if `Open` fails due to `EACCES` or `EPERM`:

```
output: permission denied on /dev/uinput — add yourself to the 'uinput' group and ensure the udev rule is in place
```

---

## Behaviours

### OUT-01 — Open creates a visible virtual keyboard device
**Given** the calling process has write access to `/dev/uinput`  
**When** `Open()` is called  
**Then** a new device named `"evmap virtual keyboard"` appears under `/dev/input/` and `Open` returns a non-nil `*Device` with nil error

### OUT-02 — WriteEvent followed by WriteSyn delivers the key event to readers
**Given** an open `*Device` and a consumer process reading from the virtual device  
**When** `WriteEvent(evKey, KEY_W, 1)` then `WriteSyn()` are called  
**Then** the consumer observes a `KEY_W` press event

### OUT-03 — Press and release are delivered as separate events
**Given** an open `*Device`  
**When** `WriteEvent(evKey, KEY_W, 1)` + `WriteSyn()`, then `WriteEvent(evKey, KEY_W, 0)` + `WriteSyn()`  
**Then** the consumer observes a press event followed by a release event, in order

### OUT-04 — Auto-repeat is forwarded
**Given** an open `*Device`  
**When** `WriteEvent(evKey, KEY_W, 2)` + `WriteSyn()`  
**Then** the consumer observes an auto-repeat event with `value=2`

### OUT-05 — Close removes the virtual device
**Given** an open `*Device`  
**When** `Close()` is called  
**Then** the device disappears from `/dev/input/` and subsequent reads from the consumer's fd return an error

### OUT-06 — Open fails gracefully when /dev/uinput is not accessible
**Given** the calling process lacks write permission on `/dev/uinput`  
**When** `Open()` is called  
**Then** a non-nil error is returned containing `"open /dev/uinput"`
