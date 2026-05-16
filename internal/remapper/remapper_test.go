package remapper

import (
	"context"
	"sync"
	"testing"
	"time"

	"evmap/internal/config"
	"evmap/internal/input"
)

// ---- mock implementations ----

// mockInput implements InputDevice for unit testing.
// It sends a fixed list of events to the channel and then blocks until the
// context is cancelled (so that Run's select can observe ctx.Done()).
type mockInput struct {
	events []input.InputEvent
	mu     sync.Mutex
	closed bool
	sent   chan struct{} // closed once every event has been handed to ch
}

func newMockInput(events ...input.InputEvent) *mockInput {
	return &mockInput{events: events, sent: make(chan struct{})}
}

func (m *mockInput) ReadEvents(ctx context.Context, ch chan<- input.InputEvent) {
	allSent := false
	defer func() {
		if !allSent {
			close(m.sent)
		}
	}()
	for _, ev := range m.events {
		select {
		case ch <- ev:
		case <-ctx.Done():
			return
		}
	}
	allSent = true
	close(m.sent) // signal that all events have been handed to ch
	<-ctx.Done()
}

func (m *mockInput) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockInput) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// writeCall records a single WriteEvent call.
type writeCall struct {
	evType uint16
	code   uint16
	value  int32
}

// mockOutput implements OutputDevice for unit testing.
type mockOutput struct {
	mu       sync.Mutex
	events   []writeCall
	synCount int
	closed   bool
}

func (m *mockOutput) WriteEvent(evType, code uint16, value int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, writeCall{evType, code, value})
	return nil
}

func (m *mockOutput) WriteSyn() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.synCount++
	return nil
}

func (m *mockOutput) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockOutput) keyEvents() []writeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]writeCall, 0, len(m.events))
	for _, e := range m.events {
		if e.evType == input.EvKey {
			result = append(result, e)
		}
	}
	return result
}

func (m *mockOutput) getSynCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.synCount
}

func (m *mockOutput) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// mockFocus implements focus.Tracker for unit testing.
type mockFocus struct {
	focused bool
}

func (f *mockFocus) IsFocused(_ string) (bool, error) { return f.focused, nil }
func (f *mockFocus) Close() error                     { return nil }

// ---- helpers ----

const (
	keyUp    uint16 = 103 // KEY_UP
	keyDown  uint16 = 108 // KEY_DOWN
	keyLeft  uint16 = 105 // KEY_LEFT
	keyRight uint16 = 106 // KEY_RIGHT
	keyW     uint16 = 17  // KEY_W
	keyS     uint16 = 31  // KEY_S
	keyA     uint16 = 30  // KEY_A
	keyD     uint16 = 32  // KEY_D
)

// runRemapper runs r.Run in a goroutine and returns a function that cancels
// the context and waits (up to 2 s) for Run to return.
func runRemapper(t *testing.T, r *Remapper) (waitForSent func(), stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	in := r.in.(*mockInput)
	waitForSent = func() {
		select {
		case <-in.sent:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for mockInput to send all events")
		}
	}
	stop = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for Run to return")
		}
	}
	return waitForSent, stop
}

// ---- tests ----

// TestRMP01_ConfiguredKeyRemappedWhenFocused codifies RMP-01.
func TestRMP01_ConfiguredKeyRemappedWhenFocused(t *testing.T) {
	in := newMockInput(input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 1})
	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "up", To: "w"}}

	r, err := New(in, out, &mockFocus{focused: true}, "game", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	waitForSent, stop := runRemapper(t, r)
	waitForSent()
	stop()

	evts := out.keyEvents()
	if len(evts) != 1 {
		t.Fatalf("got %d key events, want 1", len(evts))
	}
	if evts[0].code != keyW {
		t.Errorf("WriteEvent code = %d, want %d (KEY_W)", evts[0].code, keyW)
	}
}

// TestRMP02_UnmappedKeyPassedThroughUnchanged codifies RMP-02.
func TestRMP02_UnmappedKeyPassedThroughUnchanged(t *testing.T) {
	in := newMockInput(input.InputEvent{Type: input.EvKey, Code: keyA, Value: 1})
	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "up", To: "w"}}

	r, err := New(in, out, nil, "", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	waitForSent, stop := runRemapper(t, r)
	waitForSent()
	stop()

	evts := out.keyEvents()
	if len(evts) != 1 {
		t.Fatalf("got %d key events, want 1", len(evts))
	}
	if evts[0].code != keyA {
		t.Errorf("WriteEvent code = %d, want %d (KEY_A, unchanged)", evts[0].code, keyA)
	}
}

// TestRMP03_RemappingSuppressedWhenNotFocused codifies RMP-03.
func TestRMP03_RemappingSuppressedWhenNotFocused(t *testing.T) {
	in := newMockInput(input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 1})
	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "up", To: "w"}}

	r, err := New(in, out, &mockFocus{focused: false}, "game", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	waitForSent, stop := runRemapper(t, r)
	waitForSent()
	stop()

	evts := out.keyEvents()
	if len(evts) != 1 {
		t.Fatalf("got %d key events, want 1", len(evts))
	}
	if evts[0].code != keyUp {
		t.Errorf("WriteEvent code = %d, want %d (KEY_UP, original)", evts[0].code, keyUp)
	}
}

// TestRMP04_BothPressAndReleaseAreRemapped codifies RMP-04.
func TestRMP04_BothPressAndReleaseAreRemapped(t *testing.T) {
	in := newMockInput(
		input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 1},
		input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 0},
	)
	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "up", To: "w"}}

	r, err := New(in, out, nil, "", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	waitForSent, stop := runRemapper(t, r)
	waitForSent()
	stop()

	evts := out.keyEvents()
	if len(evts) != 2 {
		t.Fatalf("got %d key events, want 2", len(evts))
	}
	if evts[0].code != keyW || evts[0].value != 1 {
		t.Errorf("press: got code=%d value=%d, want code=%d value=1", evts[0].code, evts[0].value, keyW)
	}
	if evts[1].code != keyW || evts[1].value != 0 {
		t.Errorf("release: got code=%d value=%d, want code=%d value=0", evts[1].code, evts[1].value, keyW)
	}
}

// TestRMP05_AutoRepeatIsRemapped codifies RMP-05.
func TestRMP05_AutoRepeatIsRemapped(t *testing.T) {
	in := newMockInput(input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 2})
	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "up", To: "w"}}

	r, err := New(in, out, nil, "", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	waitForSent, stop := runRemapper(t, r)
	waitForSent()
	stop()

	evts := out.keyEvents()
	if len(evts) != 1 {
		t.Fatalf("got %d key events, want 1", len(evts))
	}
	if evts[0].code != keyW || evts[0].value != 2 {
		t.Errorf("repeat: got code=%d value=%d, want code=%d value=2", evts[0].code, evts[0].value, keyW)
	}
}

// TestRMP06_MultipleKeymapsAllApplied codifies RMP-06.
func TestRMP06_MultipleKeymapsAllApplied(t *testing.T) {
	in := newMockInput(
		input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 1},
		input.InputEvent{Type: input.EvKey, Code: keyDown, Value: 1},
		input.InputEvent{Type: input.EvKey, Code: keyLeft, Value: 1},
		input.InputEvent{Type: input.EvKey, Code: keyRight, Value: 1},
	)
	out := &mockOutput{}
	keymaps := []config.Keymap{
		{From: "up", To: "w"},
		{From: "down", To: "s"},
		{From: "left", To: "a"},
		{From: "right", To: "d"},
	}

	r, err := New(in, out, nil, "", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	waitForSent, stop := runRemapper(t, r)
	waitForSent()
	stop()

	evts := out.keyEvents()
	if len(evts) != 4 {
		t.Fatalf("got %d key events, want 4", len(evts))
	}
	want := []uint16{keyW, keyS, keyA, keyD}
	for i, w := range want {
		if evts[i].code != w {
			t.Errorf("event[%d]: got code=%d, want %d", i, evts[i].code, w)
		}
	}
}

// TestRMP07_ShutdownCallsCloseOnBothDevices codifies RMP-07.
func TestRMP07_ShutdownCallsCloseOnBothDevices(t *testing.T) {
	in := newMockInput()
	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "up", To: "w"}}

	r, err := New(in, out, nil, "", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, stop := runRemapper(t, r)
	stop()

	if !in.isClosed() {
		t.Error("in.Close() was not called before Run returned")
	}
	if !out.isClosed() {
		t.Error("out.Close() was not called before Run returned")
	}
}

// TestRMP08_ReloadReplacesKeymapWithoutRestart codifies RMP-08.
func TestRMP08_ReloadReplacesKeymapWithoutRestart(t *testing.T) {
	// Set up the input to send KEY_UP after we've had a chance to reload.
	// We use an extra sync channel to control when the event is sent.
	trigger := make(chan struct{})
	defer close(trigger)

	in := &mockInput{sent: make(chan struct{})}
	in.events = []input.InputEvent{
		{Type: input.EvKey, Code: keyUp, Value: 1},
	}

	// Override ReadEvents to wait for the trigger before sending.
	var triggerOnce sync.Once
	_ = triggerOnce
	customIn := &triggeredInput{
		events:  in.events,
		trigger: trigger,
		sent:    in.sent,
	}

	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "up", To: "w"}}

	r, err := New(customIn, out, nil, "", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	// Reload to map up→s before the event is emitted.
	if err := r.Reload([]config.Keymap{{From: "up", To: "s"}}); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	// Now let the event flow.
	trigger <- struct{}{}

	// Wait for the event to be sent.
	select {
	case <-in.sent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event to be sent")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to return")
	}

	evts := out.keyEvents()
	if len(evts) != 1 {
		t.Fatalf("got %d key events, want 1", len(evts))
	}
	wantCode, _ := config.LookupKeyCode("s")
	if evts[0].code != wantCode {
		t.Errorf("after reload: code = %d, want %d (KEY_S)", evts[0].code, wantCode)
	}
}

// triggeredInput holds events until the trigger channel receives a signal.
type triggeredInput struct {
	events  []input.InputEvent
	trigger <-chan struct{}
	sent    chan struct{}
}

func (t *triggeredInput) ReadEvents(ctx context.Context, ch chan<- input.InputEvent) {
	allSent := false
	defer func() {
		if !allSent {
			close(t.sent)
		}
	}()
	// Wait for the trigger (or ctx cancellation).
	select {
	case <-t.trigger:
	case <-ctx.Done():
		return
	}
	for _, ev := range t.events {
		select {
		case ch <- ev:
		case <-ctx.Done():
			return
		}
	}
	allSent = true
	close(t.sent)
	<-ctx.Done()
}

func (t *triggeredInput) Close() error { return nil }

// TestRMP09_FailedReloadLeavesKeymapUnchanged codifies RMP-09.
func TestRMP09_FailedReloadLeavesKeymapUnchanged(t *testing.T) {
	in := newMockInput(input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 1})
	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "up", To: "w"}}

	r, err := New(in, out, nil, "", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Attempt reload with invalid key name — must return error.
	reloadErr := r.Reload([]config.Keymap{{From: "up", To: "nosuchkey"}})
	if reloadErr == nil {
		t.Fatal("Reload() with unknown key: error = nil, want error")
	}

	// Run and verify the original mapping (up→w) is still active.
	waitForSent, stop := runRemapper(t, r)
	waitForSent()
	stop()

	evts := out.keyEvents()
	if len(evts) != 1 {
		t.Fatalf("got %d key events, want 1", len(evts))
	}
	if evts[0].code != keyW {
		t.Errorf("after failed reload: code = %d, want %d (KEY_W)", evts[0].code, keyW)
	}
}

// TestRMP10_WriteSynCalledOnceAfterEveryKeyEvent codifies RMP-10.
func TestRMP10_WriteSynCalledOnceAfterEveryKeyEvent(t *testing.T) {
	in := newMockInput(
		input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 1},
		input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 0},
		input.InputEvent{Type: input.EvKey, Code: keyUp, Value: 2},
	)
	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "up", To: "w"}}

	r, err := New(in, out, nil, "", keymaps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	waitForSent, stop := runRemapper(t, r)
	waitForSent()
	stop()

	evts := out.keyEvents()
	if len(evts) != 3 {
		t.Fatalf("got %d key events, want 3", len(evts))
	}
	synCount := out.getSynCount()
	if synCount != 3 {
		t.Errorf("WriteSyn called %d times, want 3 (once per key event)", synCount)
	}
}

// TestRMPNew_UnknownKeyNameReturnsError verifies New() rejects bad key names.
func TestRMPNew_UnknownKeyNameReturnsError(t *testing.T) {
	in := newMockInput()
	out := &mockOutput{}
	keymaps := []config.Keymap{{From: "nosuchkey", To: "w"}}

	r, err := New(in, out, nil, "", keymaps)
	if err == nil {
		t.Fatal("New() with unknown key: error = nil, want error")
	}
	if r != nil {
		t.Error("New() with unknown key: returned non-nil Remapper")
	}
}
