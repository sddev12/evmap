package input

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// EVIOCGRAB grabs/releases exclusive access to a device. _IOW('E', 0x90, int).
const EVIOCGRAB = 0x40044590

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

// Device represents an exclusively-grabbed evdev keyboard input device.
type Device struct {
	fd   int
	file *os.File
	path string
}

// Open opens the evdev device at path and takes an exclusive grab so that no
// other process receives events from it while it is held.
func Open(path string) (*Device, error) {
	// unix.Open is a direct syscall wrapper for open(2).
	// O_RDONLY — we only need to read events, never write to the device.
	// O_NONBLOCK — the fd will not block when no data is available; reads
	//   return EAGAIN immediately instead. This is required for epoll-driven I/O.
	// 0 — file-creation mode bits; ignored because we are opening an existing file.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	// unix.IoctlSetInt issues an ioctl(2) syscall on fd, passing the integer 1
	// as the argument. EVIOCGRAB is a Linux evdev ioctl: value 1 means "grab"
	// (take exclusive ownership), so no other process — including the X server
	// or compositor — will receive events from this device while we hold it.
	if err := unix.IoctlSetInt(fd, EVIOCGRAB, 1); err != nil {
		unix.Close(fd) // release the fd before returning the error
		return nil, fmt.Errorf("grab %s: %w", path, err)
	}

	return &Device{
		fd: fd,
		// os.NewFile wraps the raw file descriptor in an *os.File so that
		// encoding/binary.Read can use it as an io.Reader without us having
		// to implement the read loop manually with unix.Read.
		file: os.NewFile(uintptr(fd), path),
		path: path,
	}, nil
}

// Close releases the exclusive grab and closes the underlying file descriptor.
func (d *Device) Close() error {
	// Passing 0 to EVIOCGRAB releases the exclusive grab, making the device
	// available to other processes (e.g. the compositor) again.
	unix.IoctlSetInt(d.fd, EVIOCGRAB, 0) //nolint:errcheck — best-effort on close
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
				return
			}
			// Drop EV_MSC (raw scan codes) — uinput does not need them.
			if ev.Type == EvMsc {
				continue
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}
}
