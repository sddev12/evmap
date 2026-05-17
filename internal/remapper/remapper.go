package remapper

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"evmap/internal/config"
	"evmap/internal/focus"
	"evmap/internal/input"
)

// InputDevice reads keyboard events from a physical device.
type InputDevice interface {
	ReadEvents(ctx context.Context, ch chan<- input.InputEvent)
	Close() error
}

// OutputDevice writes keyboard events to a virtual device.
type OutputDevice interface {
	WriteEvent(timeSec, timeUsec int64, evType, code uint16, value int32) error
	WriteSyn(timeSec, timeUsec int64) error
	Close() error
}

// Remapper is the top-level remapping engine.
type Remapper struct {
	in           InputDevice
	out          OutputDevice
	focusTracker focus.Tracker
	targetTitle  string
	mu           sync.RWMutex
	keymap       map[uint16]uint16
}

// compileKeymap converts []config.Keymap to a map[uint16]uint16 lookup table.
// Returns an error (without modifying any state) if a key name is unknown.
func compileKeymap(keymaps []config.Keymap) (map[uint16]uint16, error) {
	m := make(map[uint16]uint16, len(keymaps))
	for _, km := range keymaps {
		fromCode, ok := config.LookupKeyCode(km.From)
		if !ok {
			return nil, fmt.Errorf("unknown key name %q", km.From)
		}
		toCode, ok := config.LookupKeyCode(km.To)
		if !ok {
			return nil, fmt.Errorf("unknown key name %q", km.To)
		}
		m[fromCode] = toCode
	}
	return m, nil
}

// New creates a Remapper from the given components.
// keymaps is the validated list from config; it is compiled into an internal
// lookup table (map[uint16]uint16) keyed by input key code.
// focusTracker may be nil to disable focus filtering.
// targetTitle is the window title substring to match against IsFocused;
// it is only consulted when focusTracker is non-nil.
func New(in InputDevice, out OutputDevice, focusTracker focus.Tracker, targetTitle string, keymaps []config.Keymap) (*Remapper, error) {
	km, err := compileKeymap(keymaps)
	if err != nil {
		return nil, fmt.Errorf("compile keymap: %w", err)
	}
	return &Remapper{
		in:           in,
		out:          out,
		focusTracker: focusTracker,
		targetTitle:  targetTitle,
		keymap:       km,
	}, nil
}

// Run starts the remapping loop and blocks until ctx is cancelled.
// It returns nil on clean shutdown.
func (r *Remapper) Run(ctx context.Context) error {
	eventCh := make(chan input.InputEvent, 64)
	go r.in.ReadEvents(ctx, eventCh)

	r.mu.RLock()
	keymapLen := len(r.keymap)
	r.mu.RUnlock()
	slog.Info("remapper started", "keymaps", keymapLen)

	for {
		select {
		case ev := <-eventCh:
			r.processEvent(ev)
		case <-ctx.Done():
		drain:
			for {
				select {
				case ev := <-eventCh:
					r.processEvent(ev)
				default:
					break drain
				}
			}
			r.in.Close()  //nolint:errcheck
			r.out.Close() //nolint:errcheck
			slog.Info("remapper stopped")
			return nil
		}
	}
}

// processEvent handles a single input event: forwarding, suppressing, or remapping it.
func (r *Remapper) processEvent(ev input.InputEvent) {
	// Forward SYN events as-is to preserve original event batching.
	if ev.Type == input.EvSyn {
		slog.Debug("sync event")
		r.out.WriteSyn(ev.TimeSec, ev.TimeUsec) //nolint:errcheck
		return
	}

	// Forward non-key events (like EV_MSC scan codes) unconditionally.
	if ev.Type != input.EvKey {
		slog.Debug("non-key event", "type", ev.Type, "code", ev.Code, "value", ev.Value)
		r.out.WriteEvent(ev.TimeSec, ev.TimeUsec, ev.Type, ev.Code, ev.Value) //nolint:errcheck
		return
	}

	// Check if we should bypass remapping due to window focus.
	if r.focusTracker != nil {
		focused, err := r.focusTracker.IsFocused(r.targetTitle)
		if err != nil || !focused {
			// Game not in focus — emit original event unchanged so keyboard works normally.
			slog.Debug("key not remapped (not focused)", "code", ev.Code, "value", ev.Value)
			r.out.WriteEvent(ev.TimeSec, ev.TimeUsec, ev.Type, ev.Code, ev.Value) //nolint:errcheck
			return
		}
	}

	// Apply keymap if this key is mapped, otherwise pass through unchanged.
	r.mu.RLock()
	mapped, ok := r.keymap[ev.Code]
	r.mu.RUnlock()

	outCode := ev.Code
	if ok {
		slog.Debug("key remapped", "from", ev.Code, "to", mapped, "value", ev.Value)
		outCode = mapped
	} else {
		slog.Debug("key pass-through", "code", ev.Code, "value", ev.Value)
	}

	r.out.WriteEvent(ev.TimeSec, ev.TimeUsec, ev.Type, outCode, ev.Value) //nolint:errcheck
}

// Reload replaces the active keymap with the provided one without restarting
// the loop. Safe to call from a signal handler goroutine.
// On failure the previous keymap remains active and an error is returned.
func (r *Remapper) Reload(keymaps []config.Keymap) error {
	km, err := compileKeymap(keymaps)
	if err != nil {
		slog.Warn("config reload failed", "err", err)
		return fmt.Errorf("compile keymap: %w", err)
	}
	r.mu.Lock()
	r.keymap = km
	r.mu.Unlock()
	slog.Info("config reloaded")
	return nil
}
