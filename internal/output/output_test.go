//go:build integration
// +build integration

package output

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	virtualKeyboardName = "evmap virtual keyboard"
	waitTimeout         = 5 * time.Second
)

// TestOUT01_OpenCreatesVisibleVirtualKeyboard codifies OUT-01.
func TestOUT01_OpenCreatesVisibleVirtualKeyboard(t *testing.T) {
	before, err := listVirtualKeyboardEventPaths()
	if err != nil {
		t.Fatalf("list existing virtual devices: %v", err)
	}

	dev := openOutputDeviceOrSkip(t)
	t.Cleanup(func() {
		_ = dev.Close()
	})

	path, err := waitForNewVirtualKeyboard(before, waitTimeout)
	if err != nil {
		t.Fatalf("wait for virtual keyboard: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("virtual keyboard path not visible at %s: %v", path, err)
	}
}

// TestOUT02_WriteEventThenWriteSynDeliversKeyPress codifies OUT-02.
func TestOUT02_WriteEventThenWriteSynDeliversKeyPress(t *testing.T) {
	dev, _, fd := openDeviceAndConsumerOrSkip(t)
	t.Cleanup(func() {
		_ = unix.Close(fd)
		_ = dev.Close()
	})

	if err := dev.WriteEvent(evKey, unix.KEY_W, 1); err != nil {
		t.Fatalf("write key press: %v", err)
	}
	if err := dev.WriteSyn(); err != nil {
		t.Fatalf("write syn: %v", err)
	}

	value := readKeyValue(t, fd, unix.KEY_W, waitTimeout)
	if value != 1 {
		t.Fatalf("expected KEY_W press value=1, got %d", value)
	}
}

// TestOUT03_PressAndReleaseDeliveredInOrder codifies OUT-03.
func TestOUT03_PressAndReleaseDeliveredInOrder(t *testing.T) {
	dev, _, fd := openDeviceAndConsumerOrSkip(t)
	t.Cleanup(func() {
		_ = unix.Close(fd)
		_ = dev.Close()
	})

	if err := dev.WriteEvent(evKey, unix.KEY_W, 1); err != nil {
		t.Fatalf("write key press: %v", err)
	}
	if err := dev.WriteSyn(); err != nil {
		t.Fatalf("write syn after press: %v", err)
	}
	if err := dev.WriteEvent(evKey, unix.KEY_W, 0); err != nil {
		t.Fatalf("write key release: %v", err)
	}
	if err := dev.WriteSyn(); err != nil {
		t.Fatalf("write syn after release: %v", err)
	}

	values := readKeyValues(t, fd, unix.KEY_W, 2, waitTimeout)
	if values[0] != 1 || values[1] != 0 {
		t.Fatalf("expected ordered press/release [1 0], got %v", values)
	}
}

// TestOUT04_AutoRepeatForwarded codifies OUT-04.
func TestOUT04_AutoRepeatForwarded(t *testing.T) {
	dev, _, fd := openDeviceAndConsumerOrSkip(t)
	t.Cleanup(func() {
		_ = unix.Close(fd)
		_ = dev.Close()
	})

	if err := dev.WriteEvent(evKey, unix.KEY_W, 2); err != nil {
		t.Fatalf("write key repeat: %v", err)
	}
	if err := dev.WriteSyn(); err != nil {
		t.Fatalf("write syn: %v", err)
	}

	value := readKeyValue(t, fd, unix.KEY_W, waitTimeout)
	if value != 2 {
		t.Fatalf("expected KEY_W repeat value=2, got %d", value)
	}
}

// TestOUT05_CloseRemovesVirtualKeyboardAndBreaksConsumerReads codifies OUT-05.
func TestOUT05_CloseRemovesVirtualKeyboardAndBreaksConsumerReads(t *testing.T) {
	dev, path, fd := openDeviceAndConsumerOrSkip(t)
	t.Cleanup(func() {
		_ = unix.Close(fd)
	})

	if err := dev.Close(); err != nil {
		t.Fatalf("close virtual keyboard: %v", err)
	}

	if err := waitForPathMissing(path, waitTimeout); err != nil {
		t.Fatalf("wait for device removal: %v", err)
	}

	deadline := time.Now().Add(waitTimeout)
	buf := make([]byte, binary.Size(kernelEvent{}))
	for time.Now().Before(deadline) {
		_, err := unix.Read(fd, buf)
		if err != nil && !errors.Is(err, unix.EAGAIN) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("expected read error after close")
}

func openOutputDeviceOrSkip(t *testing.T) *Device {
	t.Helper()

	dev, err := Open()
	if err != nil {
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOENT) {
			t.Skipf("integration environment unavailable: %v", err)
		}
		t.Fatalf("open output device: %v", err)
	}
	return dev
}

func openDeviceAndConsumerOrSkip(t *testing.T) (*Device, string, int) {
	t.Helper()

	before, err := listVirtualKeyboardEventPaths()
	if err != nil {
		t.Fatalf("list existing virtual devices: %v", err)
	}

	dev := openOutputDeviceOrSkip(t)

	path, err := waitForNewVirtualKeyboard(before, waitTimeout)
	if err != nil {
		_ = dev.Close()
		t.Fatalf("wait for virtual keyboard path: %v", err)
	}

	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		_ = dev.Close()
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
			t.Skipf("cannot open consumer fd for %s: %v", path, err)
		}
		t.Fatalf("open consumer fd for %s: %v", path, err)
	}

	return dev, path, fd
}

func listVirtualKeyboardEventPaths() (map[string]struct{}, error) {
	paths := map[string]struct{}{}

	events, err := filepath.Glob("/sys/class/input/event*")
	if err != nil {
		return nil, err
	}

	for _, event := range events {
		nameBytes, err := os.ReadFile(filepath.Join(event, "device", "name"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(nameBytes)) != virtualKeyboardName {
			continue
		}
		eventName := filepath.Base(event)
		paths[filepath.Join("/dev/input", eventName)] = struct{}{}
	}

	return paths, nil
}

func waitForNewVirtualKeyboard(before map[string]struct{}, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		current, err := listVirtualKeyboardEventPaths()
		if err != nil {
			return "", err
		}
		for path := range current {
			if _, found := before[path]; !found {
				return path, nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	return "", fmt.Errorf("timeout waiting for %q under /dev/input", virtualKeyboardName)
}

func waitForPathMissing(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("path %s still present after timeout", path)
}

func readKeyValue(t *testing.T, fd int, code uint16, timeout time.Duration) int32 {
	t.Helper()
	return readKeyValues(t, fd, code, 1, timeout)[0]
}

func readKeyValues(t *testing.T, fd int, code uint16, count int, timeout time.Duration) []int32 {
	t.Helper()

	deadline := time.Now().Add(timeout)
	values := make([]int32, 0, count)
	for len(values) < count {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %d key events (got %d)", count, len(values))
		}

		ev, err := readOneEvent(fd, time.Until(deadline))
		if err != nil {
			t.Fatalf("read input event: %v", err)
		}
		if ev.Type == evKey && ev.Code == code {
			values = append(values, ev.Value)
		}
	}

	return values
}

func readOneEvent(fd int, timeout time.Duration) (kernelEvent, error) {
	if timeout <= 0 {
		return kernelEvent{}, fmt.Errorf("timeout waiting for input event")
	}

	pollTimeout := int(timeout / time.Millisecond)
	if pollTimeout < 1 {
		pollTimeout = 1
	}

	fds := []unix.PollFd{
		{
			Fd:     int32(fd),
			Events: unix.POLLIN | unix.POLLERR | unix.POLLHUP,
		},
	}

	n, err := unix.Poll(fds, pollTimeout)
	if err != nil {
		return kernelEvent{}, err
	}
	if n == 0 {
		return kernelEvent{}, fmt.Errorf("poll timeout waiting for input event")
	}

	buf := make([]byte, binary.Size(kernelEvent{}))
	readN, err := unix.Read(fd, buf)
	if err != nil {
		return kernelEvent{}, err
	}
	if readN != len(buf) {
		return kernelEvent{}, fmt.Errorf("short read: got %d bytes", readN)
	}

	var ev kernelEvent
	if err := binary.Read(bytes.NewReader(buf), binary.NativeEndian, &ev); err != nil {
		return kernelEvent{}, err
	}
	return ev, nil
}
