package input

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// EVIOCGRAB grabs/releases exclusive access to a device. _IOW('E', 0x90, int).
const EVIOCGRAB = 0x40044590

// EVIOCGNAME gets the device name string. _IOR('E', 0x06, char[size]).
const eviocgnameBase = 0x06

// EVIOCGKEY gets the current key state bitmap. _IOR('E', 0x18, char[size]).
const eviocgkeyBase = 0x18

// KEY_MAX is the maximum key code (from linux/input-event-codes.h).
const keyMax = 0x2FF

const keyCodeA uint16 = 30

const (
	iocNrbits   = 8
	iocTypebits = 8
	iocSizebits = 14

	iocNrshift   = 0
	iocTypeshift = iocNrshift + iocNrbits
	iocSizeshift = iocTypeshift + iocTypebits
	iocDirshift  = iocSizeshift + iocSizebits

	iocRead = 2

	eviocgbitBase = 0x20
)

var (
	globEventDevices = filepath.Glob
	openDevice       = unix.Open
	closeDevice      = unix.Close
	ioctlSetInt      = unix.IoctlSetInt
	ioctlGetBits     = ioctlGetEventBits
	syscallIoctl     = unix.Syscall
)

// Event type constants from linux/input.h.
const (
	EvSyn uint16 = 0x00 // sync / separator
	EvKey uint16 = 0x01 // key press / release
	EvMsc uint16 = 0x04 // miscellaneous (raw scan codes)
)

// InputEvent mirrors the kernel's struct input_event from linux/input.h.
// On 64-bit Linux it is 24 bytes: {int64 sec, int64 usec, uint16 type, uint16 code, int32 value}.
type InputEvent struct {
	TimeSec  int64
	TimeUsec int64
	Type     uint16
	Code     uint16
	Value    int32
}

// DeviceInfo holds information about a discovered input device.
type DeviceInfo struct {
	Path string // /dev/input/eventN
	Name string // Human-readable device name from EVIOCGNAME
}

// Device represents an exclusively-grabbed evdev keyboard input device.
type Device struct {
	fd   int
	file *os.File
	path string
}

// Discover returns /dev/input/event* devices that report KEY_A support, with their human-readable names.
func Discover() ([]DeviceInfo, error) {
	paths, err := globEventDevices("/dev/input/event*")
	if err != nil {
		return nil, fmt.Errorf("discover devices: %w", err)
	}

	keyboards := make([]DeviceInfo, 0, len(paths))
	for _, path := range paths {
		fd, err := openDevice(path, unix.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			continue
		}

		ok, err := hasKeyCapability(fd, keyCodeA)
		if err != nil || !ok {
			closeDevice(fd) //nolint:errcheck
			continue
		}

		// Get device name
		name, err := getDeviceName(fd)
		closeDevice(fd) //nolint:errcheck // probing only; individual close failures should not abort discovery
		if err != nil {
			// If we can't get the name, use the path as fallback
			name = path
		}

		keyboards = append(keyboards, DeviceInfo{
			Path: path,
			Name: name,
		})
	}

	return keyboards, nil
}

// Open opens the evdev device at path and takes an exclusive grab so that no
// other process receives events from it while it is held.
func Open(path string) (*Device, error) {
	// unix.Open is a direct syscall wrapper for open(2).
	// O_RDONLY — we only need to read events, never write to the device.
	// O_NONBLOCK — the fd will not block when no data is available; reads
	//   return EAGAIN immediately instead. This is required for epoll-driven I/O.
	// 0 — file-creation mode bits; ignored because we are opening an existing file.
	fd, err := openDevice(path, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
			return nil, fmt.Errorf("permission denied on %s — add yourself to the 'input' group", path)
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	// Wait for all keys to be released before grabbing the device.
	// This prevents orphaned key releases (e.g., Enter from executing the shell
	// command) from being captured and causing X11/Wayland auto-repeat issues.
	if err := waitForNeutralKeyState(fd); err != nil {
		closeDevice(fd)
		return nil, fmt.Errorf("waiting for neutral key state on %s: %w", path, err)
	}

	// unix.IoctlSetInt issues an ioctl(2) syscall on fd, passing the integer 1
	// as the argument. EVIOCGRAB is a Linux evdev ioctl: value 1 means "grab"
	// (take exclusive ownership), so no other process — including the X server
	// or compositor — will receive events from this device while we hold it.
	if err := ioctlSetInt(fd, EVIOCGRAB, 1); err != nil {
		closeDevice(fd) // release the fd before returning the error
		return nil, fmt.Errorf("grab %s: %w", path, err)
	}

	// Drain any stale events from the kernel buffer that were queued before
	// the grab. These can include unpaired key releases that confuse the
	// virtual keyboard's key state and cause phantom key repeats.
	// Must drain BEFORE wrapping fd in os.File to avoid buffering issues.
	drainBuffer(fd)

	dev := &Device{
		fd: fd,
		// os.NewFile wraps the raw file descriptor in an *os.File so that
		// encoding/binary.Read can use it as an io.Reader without us having
		// to implement the read loop manually with unix.Read.
		file: os.NewFile(uintptr(fd), path),
		path: path,
	}

	return dev, nil
}

// Close releases the exclusive grab and closes the underlying file descriptor.
func (d *Device) Close() error {
	// Passing 0 to EVIOCGRAB releases the exclusive grab, making the device
	// available to other processes (e.g. the compositor) again.
	ioctlSetInt(d.fd, EVIOCGRAB, 0) //nolint:errcheck — best-effort on close
	return d.file.Close()
}

// ReadEvents reads input_event structs from the device in a loop, forwarding
// them to ch until ctx is cancelled. It is intended to run in its own goroutine.
// EV_MSC (scan code) events are dropped; all others are forwarded.
func (d *Device) ReadEvents(ctx context.Context, ch chan<- InputEvent) {
	// unix.EpollCreate1 wraps epoll_create1(2), which creates a new epoll
	// instance and returns a file descriptor for it. The 0 flag means no
	// special options (EPOLL_CLOEXEC would be the alternative).
	// Epoll lets us block efficiently until data is available on d.fd
	// while still being able to wake up periodically to check ctx.
	epfd, err := unix.EpollCreate1(0)
	if err != nil {
		slog.Error("epoll_create1 failed", "err", err)
		return
	}
	// Close the epoll fd when ReadEvents returns so we don't leak it.
	defer unix.Close(epfd)

	// unix.EpollCtl wraps epoll_ctl(2).
	// EPOLL_CTL_ADD registers d.fd with the epoll instance.
	// The EpollEvent struct tells the kernel which events to watch for:
	//   EPOLLIN — notify us when data is available to read (a key event arrived).
	//   Fd — echoed back in the event so we know which fd is ready (useful when
	//         monitoring multiple fds on the same epoll instance).
	if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, d.fd, &unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(d.fd),
	}); err != nil {
		slog.Error("epoll_ctl failed", "err", err)
		return
	}

	// Reusable slice that epoll_wait will fill with ready events.
	// Capacity of 8 means we can process up to 8 fds per wait call;
	// we only watch one fd, so in practice n is always 0 or 1.
	epollEvents := make([]unix.EpollEvent, 8)

	for {
		if ctx.Err() != nil {
			return
		}

		// unix.EpollWait wraps epoll_wait(2). It blocks until at least one
		// watched fd is ready or the timeout expires, then fills epollEvents
		// with the ready descriptors and returns how many were written (n).
		// Timeout of 50 ms: if no key arrives within 50 ms we loop back and
		// re-check ctx.Err() so cancellation is noticed promptly.
		n, err := unix.EpollWait(epfd, epollEvents, 50)
		if err != nil {
			// EINTR means the syscall was interrupted by a signal; just retry.
			if err == unix.EINTR {
				continue
			}
			return
		}

		for range n {
			var ev InputEvent
			// binary.Read deserialises one InputEvent from the device file using
			// the host's native byte order (little-endian on x86/ARM).
			// The kernel writes exactly sizeof(struct input_event) = 24 bytes per
			// event, matching the layout of our InputEvent struct.
			if err := binary.Read(d.file, binary.NativeEndian, &ev); err != nil {
				slog.Error("failed to read input event", "err", err)
				return
			}
			// Forward all events including EV_MSC — some applications need scan codes.
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}
}

func hasKeyCapability(fd int, keyCode uint16) (bool, error) {
	bits := make([]byte, int(keyCode/8)+1)
	if err := ioctlGetBits(fd, EvKey, bits); err != nil {
		return false, err
	}
	return bits[keyCode/8]&(1<<(keyCode%8)) != 0, nil
}

func ioctlGetEventBits(fd int, evType uint16, bits []byte) error {
	if len(bits) == 0 {
		return nil
	}
	req := ioctlRead('E', uintptr(eviocgbitBase+evType), uintptr(len(bits)))
	_, _, errno := syscallIoctl(unix.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&bits[0])))
	if errno != 0 {
		return errno
	}
	return nil
}

func ioctlRead(typ byte, nr, size uintptr) uintptr {
	// Implements Linux _IOR(type, nr, size) ioctl encoding from <asm-generic/ioctl.h>.
	return (iocRead << iocDirshift) | (size << iocSizeshift) | (uintptr(typ) << iocTypeshift) | (nr << iocNrshift)
}

// getDeviceName retrieves the human-readable device name from an evdev file descriptor.
func getDeviceName(fd int) (string, error) {
	buf := make([]byte, 256) // EVIOCGNAME buffer (typical max is ~80 chars)
	req := ioctlRead('E', eviocgnameBase, uintptr(len(buf)))
	_, _, errno := syscallIoctl(unix.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return "", errno
	}
	// Trim null terminator and any trailing nulls
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i]), nil
		}
	}
	return string(buf), nil
}

// queryKeyState retrieves the current pressed key state bitmap from the kernel.
// Returns a bitmask where bit N indicates whether key N is currently pressed.
func queryKeyState(fd int) ([]byte, error) {
	// Allocate enough bytes to hold key states for all key codes up to KEY_MAX.
	// Each byte holds 8 key states (one bit per key).
	stateLen := (keyMax / 8) + 1
	state := make([]byte, stateLen)

	req := ioctlRead('E', eviocgkeyBase, uintptr(stateLen))
	_, _, errno := syscallIoctl(unix.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&state[0])))
	if errno != 0 {
		return nil, fmt.Errorf("EVIOCGKEY ioctl: %w", errno)
	}

	return state, nil
}

// countPressedKeys counts how many keys are currently pressed according to the state bitmap.
func countPressedKeys(state []byte) int {
	count := 0
	for i := 0; i < keyMax; i++ {
		if state[i/8]&(1<<(i%8)) != 0 {
			count++
		}
	}
	return count
}

// waitForNeutralKeyState waits until all keys are released before returning.
// This prevents orphaned key releases from confusing X11/Wayland auto-repeat.
// Based on keyd's device_grab implementation.
func waitForNeutralKeyState(fd int) error {
	pendingRelease := false

	for {
		state, err := queryKeyState(fd)
		if err != nil {
			return err
		}

		numPressed := countPressedKeys(state)
		if numPressed == 0 {
			break
		}

		slog.Debug("waiting for keys to be released", "count", numPressed)
		pendingRelease = true
		// Small delay to allow key up events to propagate through the kernel
		time.Sleep(10000 * time.Microsecond) // 10ms
	}

	if pendingRelease {
		// Allow the key up events to fully propagate to X11/Wayland before grabbing.
		// This gives the compositor time to see the release events and stop auto-repeat.
		slog.Debug("keys released, allowing propagation delay")
		time.Sleep(100000 * time.Microsecond) // 100ms (same as keyd)
	}

	return nil
}

// drainBuffer reads and discards all pending events from the device.
// This clears stale events (like unpaired key releases) that were queued
// before the exclusive grab, preventing them from confusing the virtual
// keyboard's key state. Uses a direct syscall to avoid blocking.
func drainBuffer(fd int) {
	var ev InputEvent
	count := 0
	buf := make([]byte, binary.Size(ev))

	for {
		// Use unix.Read directly to get proper errno handling for O_NONBLOCK
		n, err := unix.Read(fd, buf)
		if err != nil {
			// EAGAIN/EWOULDBLOCK means buffer is empty, which is what we want
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				break
			}
			// Any other error, stop draining
			break
		}
		if n == 0 {
			// EOF or no data
			break
		}
		count++
	}
	if count > 0 {
		slog.Debug("drained stale events from input buffer", "count", count)
	}
}
