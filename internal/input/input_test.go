package input

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	readEventTimeout       = 300 * time.Millisecond
	stopWaitTimeout        = 150 * time.Millisecond
	noEventTimeout         = 120 * time.Millisecond
	maxCancellationLatency = 100 * time.Millisecond
)

// TestINP01_DiscoverReturnsOnlyKeyboardDevices codifies INP-01.
func TestINP01_DiscoverReturnsOnlyKeyboardDevices(t *testing.T) {
	restore := mockInputDeps(t)
	defer restore()

	const keyboardPath = "/dev/input/event2"
	const mousePath = "/dev/input/event3"

	pathToFD := map[string]int{keyboardPath: 101, mousePath: 102}
	closed := make([]int, 0, 2)

	globEventDevices = func(pattern string) ([]string, error) {
		return []string{keyboardPath, mousePath}, nil
	}
	openDevice = func(path string, flag int, perm uint32) (int, error) {
		return pathToFD[path], nil
	}
	ioctlGetBits = func(fd int, evType uint16, bits []byte) error {
		if fd == pathToFD[keyboardPath] {
			bits[keyCodeA/8] |= 1 << (keyCodeA % 8)
		}
		return nil
	}
	closeDevice = func(fd int) error {
		closed = append(closed, fd)
		return nil
	}

	devices, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if !slices.Equal(devices, []string{keyboardPath}) {
		t.Fatalf("Discover() = %v, want [%s]", devices, keyboardPath)
	}
	if !slices.Equal(closed, []int{pathToFD[keyboardPath], pathToFD[mousePath]}) {
		t.Fatalf("close sequence = %v", closed)
	}
}

// TestINP02_DiscoverReturnsEmptySliceWhenNoKeyboardIsPresent codifies INP-02.
func TestINP02_DiscoverReturnsEmptySliceWhenNoKeyboardIsPresent(t *testing.T) {
	restore := mockInputDeps(t)
	defer restore()

	globEventDevices = func(pattern string) ([]string, error) {
		return []string{}, nil
	}

	devices, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if devices == nil {
		t.Fatal("Discover() returned nil slice, want empty non-nil slice")
	}
	if len(devices) != 0 {
		t.Fatalf("Discover() len = %d, want 0", len(devices))
	}
}

// TestINP03_OpenSucceedsOnValidPath codifies INP-03.
func TestINP03_OpenSucceedsOnValidPath(t *testing.T) {
	restore := mockInputDeps(t)
	defer restore()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	fd := int(r.Fd())
	sawGrab := false
	openDevice = func(path string, flag int, perm uint32) (int, error) {
		return fd, nil
	}
	ioctlSetInt = func(fd int, req uint, value int) error {
		if req != EVIOCGRAB {
			t.Fatalf("unexpected ioctlSetInt args: fd=%d req=%d value=%d", fd, req, value)
		}
		if value == 1 {
			sawGrab = true
		}
		return nil
	}

	d, err := Open("/dev/input/event9")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if d == nil {
		t.Fatal("Open() returned nil device")
	}
	if !sawGrab {
		t.Fatal("Open() did not issue grab ioctl")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestINP04_OpenFailsWhenGrabIsAlreadyHeld codifies INP-04.
func TestINP04_OpenFailsWhenGrabIsAlreadyHeld(t *testing.T) {
	restore := mockInputDeps(t)
	defer restore()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	fd := int(r.Fd())
	closed := false
	openDevice = func(path string, flag int, perm uint32) (int, error) {
		return fd, nil
	}
	ioctlSetInt = func(fd int, req uint, value int) error {
		return unix.EBUSY
	}
	closeDevice = func(fd int) error {
		closed = true
		return r.Close()
	}

	d, err := Open("/dev/input/event5")
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if d != nil {
		t.Fatalf("Open() device = %#v, want nil", d)
	}
	if !strings.Contains(err.Error(), "grab") {
		t.Fatalf("Open() error = %q, want substring %q", err.Error(), "grab")
	}
	if !closed {
		t.Fatal("Open() did not close fd after grab failure")
	}
}

// TestINP05_ReadEventsDeliversKeyPressAndRelease codifies INP-05.
func TestINP05_ReadEventsDeliversKeyPressAndRelease(t *testing.T) {
	d, writer := newSocketPairDevice(t)
	defer d.file.Close()
	defer writer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan InputEvent, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.ReadEvents(ctx, ch)
	}()

	writeEvent(t, writer, InputEvent{Type: EvKey, Code: keyCodeA, Value: 1})
	writeEvent(t, writer, InputEvent{Type: EvKey, Code: keyCodeA, Value: 0})

	gotPress := recvEvent(t, ch)
	gotRelease := recvEvent(t, ch)

	if gotPress.Type != EvKey || gotPress.Value != 1 {
		t.Fatalf("press event = %+v, want Type=%d Value=1", gotPress, EvKey)
	}
	if gotRelease.Type != EvKey || gotRelease.Value != 0 {
		t.Fatalf("release event = %+v, want Type=%d Value=0", gotRelease, EvKey)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(stopWaitTimeout):
		t.Fatal("ReadEvents did not stop after cancellation")
	}
}

// TestINP06_ReadEventsDropsEvMscEvents codifies INP-06.
func TestINP06_ReadEventsDropsEvMscEvents(t *testing.T) {
	d, writer := newSocketPairDevice(t)
	defer d.file.Close()
	defer writer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan InputEvent, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.ReadEvents(ctx, ch)
	}()

	writeEvent(t, writer, InputEvent{Type: EvMsc, Code: 4, Value: 1})
	writeEvent(t, writer, InputEvent{Type: EvKey, Code: keyCodeA, Value: 1})

	got := recvEvent(t, ch)
	if got.Type != EvKey || got.Value != 1 {
		t.Fatalf("event = %+v, want Type=%d Value=1", got, EvKey)
	}
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra event = %+v", extra)
	case <-time.After(noEventTimeout):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(stopWaitTimeout):
		t.Fatal("ReadEvents did not stop after cancellation")
	}
}

// TestINP07_ReadEventsStopsOnContextCancellationWithin100ms codifies INP-07.
func TestINP07_ReadEventsStopsOnContextCancellationWithin100ms(t *testing.T) {
	d, writer := newSocketPairDevice(t)
	defer d.file.Close()
	defer writer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan InputEvent, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		d.ReadEvents(ctx, ch)
	}()

	time.Sleep(10 * time.Millisecond)
	start := time.Now()
	cancel()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > maxCancellationLatency {
			t.Fatalf("ReadEvents stop time = %s, want <= %s", elapsed, maxCancellationLatency)
		}
	case <-time.After(stopWaitTimeout):
		t.Fatal("ReadEvents did not return within 100ms of cancellation")
	}
}

// TestINP08_CloseReleasesGrab codifies INP-08.
func TestINP08_CloseReleasesGrab(t *testing.T) {
	restore := mockInputDeps(t)
	defer restore()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer w.Close()

	var (
		mu       sync.Mutex
		released = map[int]bool{}
	)
	ioctlSetInt = func(fd int, req uint, value int) error {
		mu.Lock()
		defer mu.Unlock()
		if req != EVIOCGRAB {
			return nil
		}
		if value == 0 {
			released[fd] = true
			return nil
		}
		// Simulate that a new grab attempt succeeds once Close has released grab ownership.
		if value == 1 && released[fd] {
			return nil
		}
		return unix.EBUSY
	}

	d := &Device{
		fd:   int(r.Fd()),
		file: r,
		path: "/dev/input/event0",
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := ioctlSetInt(d.fd, EVIOCGRAB, 1); err != nil {
		t.Fatalf("re-grab after Close() failed: %v", err)
	}
}

func TestOpenPermissionDeniedMessage(t *testing.T) {
	restore := mockInputDeps(t)
	defer restore()

	openDevice = func(path string, flag int, perm uint32) (int, error) {
		return -1, unix.EACCES
	}

	d, err := Open("/dev/input/event4")
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if d != nil {
		t.Fatalf("Open() device = %#v, want nil", d)
	}
	want := "permission denied on /dev/input/event4 — add yourself to the 'input' group"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Open() error = %q, want substring %q", err.Error(), want)
	}
}

func mockInputDeps(t *testing.T) func() {
	t.Helper()

	origGlob := globEventDevices
	origOpen := openDevice
	origClose := closeDevice
	origIoctlSetInt := ioctlSetInt
	origIoctlGetBits := ioctlGetBits

	return func() {
		globEventDevices = origGlob
		openDevice = origOpen
		closeDevice = origClose
		ioctlSetInt = origIoctlSetInt
		ioctlGetBits = origIoctlGetBits
	}
}

func newSocketPairDevice(t *testing.T) (*Device, *os.File) {
	t.Helper()

	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("unix.Socketpair() error = %v", err)
	}

	reader := os.NewFile(uintptr(pair[0]), "reader")
	writer := os.NewFile(uintptr(pair[1]), "writer")
	return &Device{fd: pair[0], file: reader, path: "/dev/input/event-test"}, writer
}

func writeEvent(t *testing.T, writer *os.File, ev InputEvent) {
	t.Helper()
	if err := binary.Write(writer, binary.NativeEndian, &ev); err != nil {
		t.Fatalf("binary.Write() error = %v", err)
	}
}

func recvEvent(t *testing.T, ch <-chan InputEvent) InputEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(readEventTimeout):
		t.Fatal("timed out waiting for input event")
		return InputEvent{}
	}
}

func TestDiscoverIgnoresIoctlErrors(t *testing.T) {
	restore := mockInputDeps(t)
	defer restore()

	globEventDevices = func(pattern string) ([]string, error) {
		return []string{"/dev/input/event1"}, nil
	}
	openDevice = func(path string, flag int, perm uint32) (int, error) {
		return 10, nil
	}
	ioctlGetBits = func(fd int, evType uint16, bits []byte) error {
		return errors.New("ioctl failure")
	}
	closeDevice = func(fd int) error { return nil }

	devices, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("Discover() = %v, want []", devices)
	}
}
