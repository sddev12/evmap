package output

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Uinput ioctl numbers derived from linux/uinput.h using the _IOC macro:
//
//	_IOW(type, nr, size) = (IOC_WRITE<<30) | (sizeof(size)<<16) | (type<<8) | nr
//
// All three use type='U' (0x55). UI_SET_*BIT pass an int argument (4 bytes).
// UI_DEV_CREATE/DESTROY take no argument (_IO, so direction=0, size=0).
const (
	uiSetEvBit   = 0x40045564 // UI_SET_EVBIT  — register an event type we will emit
	uiSetKeyBit  = 0x40045565 // UI_SET_KEYBIT — register a key code we will emit
	uiSetMscBit  = 0x40045568 // UI_SET_MSCBIT — register a misc event code we will emit
	uiDevCreate  = 0x5501     // UI_DEV_CREATE  — instantiate the virtual device
	uiDevDestroy = 0x5502     // UI_DEV_DESTROY — tear down the virtual device
)

// keyMax is the highest key code defined in linux/input-event-codes.h.
// Registering all codes 0..keyMax lets the virtual device emit any key without
// needing to know the remapping at setup time.
const keyMax = 0x2FF

// Event type and sync constants matching linux/input-event-codes.h.
const (
	evSyn     uint16 = 0x00 // EV_SYN — synchronisation / batch boundary
	evKey     uint16 = 0x01 // EV_KEY — key press / release / repeat
	evMsc     uint16 = 0x04 // EV_MSC — miscellaneous (scan codes)
	synReport uint16 = 0x00 // SYN_REPORT sub-code: signals end of one event batch
	mscScan   uint16 = 0x04 // MSC_SCAN — raw scan code from keyboard
)

// uinputUserDev mirrors the kernel's struct uinput_user_dev from linux/uinput.h.
// Writing this struct to /dev/uinput (before UI_DEV_CREATE) sets the virtual
// device's name and identity as it will appear to udev, libinput, and X11.
// The four Abs* arrays configure absolute-axis ranges (joysticks, tablets);
// we leave them zeroed because this is a key-only device.
type uinputUserDev struct {
	Name         [80]byte  // human-readable device name shown in /proc/bus/input/devices
	BusType      uint16    // bus type  (BUS_USB=0x03, BUS_VIRTUAL=0x06, …)
	Vendor       uint16    // USB vendor ID  (0 = unspecified)
	Product      uint16    // USB product ID (0 = unspecified)
	Version      uint16    // version number (0 = unspecified)
	FfEffectsMax uint32    // force-feedback slot count; 0 for non-FF devices
	Absmax       [64]int32 // maximum value for each absolute axis; unused here
	Absmin       [64]int32 // minimum value for each absolute axis; unused here
	Absfuzz      [64]int32 // noise threshold for each axis; unused here
	Absflat      [64]int32 // dead-zone size for each axis; unused here
}

// kernelEvent is the 24-byte struct the kernel reads when we write to the
// uinput fd. It exactly matches linux's struct input_event layout on 64-bit.
type kernelEvent struct {
	TimeSec  int64  // seconds part of the event timestamp
	TimeUsec int64  // microseconds part of the event timestamp
	Type     uint16 // event type (evKey, evSyn, …)
	Code     uint16 // key code (for EV_KEY) or axis code (for EV_ABS)
	Value    int32  // 1=pressed, 0=released, 2=autorepeat (for EV_KEY events)
}

// Device represents a uinput virtual keyboard device.
type Device struct {
	fd   int      // raw file descriptor for /dev/uinput
	file *os.File // fd wrapped as an io.Writer so binary.Write can use it
}

var unixOpen = unix.Open

// Open creates a new uinput virtual keyboard device.
// The kernel registers it under /dev/input/eventN and notifies udev/libinput,
// making it visible to the rest of the system as an ordinary keyboard.
func Open() (*Device, error) {
	// unix.Open wraps open(2).
	// O_WRONLY  — we only write events to uinput, never read from it.
	// O_NONBLOCK — writes return EAGAIN instead of blocking if the kernel
	//   event buffer is temporarily full (in practice they complete instantly).
	// 0 — creation mode bits; irrelevant because we are opening an existing node.
	fd, err := unixOpen("/dev/uinput", unix.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
			return nil, fmt.Errorf("open /dev/uinput: output: permission denied on /dev/uinput — add yourself to the 'uinput' group and ensure the udev rule is in place: %w", err)
		}
		return nil, fmt.Errorf("open /dev/uinput: %w", err)
	}

	// UI_SET_EVBIT registers the event types this virtual device will produce.
	// We need three types:
	//   evSyn (0) — every event batch must end with a SYN_REPORT; without this
	//               the kernel will not deliver preceding events to consumers.
	//   evKey (1) — carries key press/release/repeat events.
	//   evMsc (4) — carries miscellaneous events like scan codes.
	// unix.IoctlSetInt issues ioctl(fd, UI_SET_EVBIT, bitValue).
	for _, evBit := range []int{int(evSyn), int(evKey), int(evMsc)} {
		if err := unix.IoctlSetInt(fd, uiSetEvBit, evBit); err != nil {
			unix.Close(fd)
			return nil, fmt.Errorf("UI_SET_EVBIT %d: %w", evBit, err)
		}
	}

	// UI_SET_KEYBIT registers each individual key code the device may emit.
	// The kernel will silently discard any key event whose code was not
	// registered here, so we pre-register the entire range 0..KEY_MAX.
	for code := 0; code <= keyMax; code++ {
		if err := unix.IoctlSetInt(fd, uiSetKeyBit, code); err != nil {
			unix.Close(fd)
			return nil, fmt.Errorf("UI_SET_KEYBIT %d: %w", code, err)
		}
	}

	// UI_SET_MSCBIT registers the MSC_SCAN code so the device can emit scan codes.
	if err := unix.IoctlSetInt(fd, uiSetMscBit, int(mscScan)); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("UI_SET_MSCBIT %d: %w", mscScan, err)
	}

	// os.NewFile wraps the raw fd as an *os.File so binary.Write can use it
	// as an io.Writer, avoiding manual byte-slice marshalling.
	file := os.NewFile(uintptr(fd), "/dev/uinput")

	// Build the uinput_user_dev that names and identifies the virtual device.
	dev := uinputUserDev{}
	copy(dev.Name[:], "evmap virtual keyboard") // null-padded automatically by zero value
	dev.BusType = 0x03                          // BUS_USB — a common synthetic-device convention

	// binary.Write serialises dev field-by-field using native byte order,
	// producing a flat 1116-byte blob that matches the C struct layout the
	// kernel expects. This must be written before UI_DEV_CREATE.
	if err := binary.Write(file, binary.NativeEndian, &dev); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write uinput_user_dev: %w", err)
	}

	// UI_DEV_CREATE tells the kernel to instantiate the virtual device using
	// the event-bit / key-bit masks set above and the uinput_user_dev just
	// written. After this ioctl returns the device is live at /dev/input/eventN
	// and visible to any program that reads input devices.
	if err := unix.IoctlSetInt(fd, uiDevCreate, 0); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("UI_DEV_CREATE: %w", err)
	}

	return &Device{fd: fd, file: file}, nil
}

// WriteEvent writes a single input_event to the uinput device.
// evType is the event class (evKey for key events), code is the key code, and
// value is 1 for press, 0 for release, or 2 for autorepeat.
// After one or more WriteEvent calls you must call WriteSyn to flush the batch.
func (d *Device) WriteEvent(timeSec, timeUsec int64, evType, code uint16, value int32) error {
	ev := kernelEvent{
		// Preserve the original timestamp from the physical device so events
		// that belong together (e.g. MSC_SCAN + KEY_ENTER) share the same time.
		TimeSec:  timeSec,
		TimeUsec: timeUsec,
		Type:     evType,
		Code:     code,
		Value:    value,
	}
	// binary.Write serialises ev as 24 bytes (matching struct input_event) and
	// writes them to the uinput fd. The kernel parses the struct and routes the
	// event to every process that has opened the virtual device for reading.
	if err := binary.Write(d.file, binary.NativeEndian, &ev); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	return nil
}

// WriteSyn emits an EV_SYN / SYN_REPORT event.
// This acts as a batch boundary: the kernel holds preceding key events until
// it sees SYN_REPORT, then delivers them all at once to reading processes.
// You must call WriteSyn after every key press or release.
func (d *Device) WriteSyn(timeSec, timeUsec int64) error {
	return d.WriteEvent(timeSec, timeUsec, evSyn, synReport, 0)
}

// Close destroys the virtual device and releases the /dev/uinput fd.
// After this returns the device disappears from /dev/input/ and udev removes it.
func (d *Device) Close() error {
	// UI_DEV_DESTROY unregisters the virtual device from the kernel input layer.
	// This must happen before the fd is closed; once closed the ioctl would fail.
	unix.IoctlSetInt(d.fd, uiDevDestroy, 0) //nolint:errcheck — best-effort on close
	return d.file.Close()
}
