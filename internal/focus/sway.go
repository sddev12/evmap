package focus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
)

// startSwaySubscribe is the function used to start the swaymsg subscribe
// process. Replaced in tests so that no real swaymsg process is needed.
var startSwaySubscribe = startSwaySpy

// swayEvent represents a single window event emitted by swaymsg subscribe.
type swayEvent struct {
	Change    string        `json:"change"`
	Container swayContainer `json:"container"`
}

type swayContainer struct {
	Name string `json:"name"`
}

// SwayTracker maintains the active Sway window title by subscribing to window
// focus events via swaymsg.
type SwayTracker struct {
	mu           sync.RWMutex
	currentTitle string
	closed       bool
	cancel       context.CancelFunc
	done         chan struct{}
}

// newSwayTracker creates a SwayTracker that reads window titles from titleCh.
// cancel must cancel the context that was passed to whatever produces titleCh.
func newSwayTracker(ctx context.Context, cancel context.CancelFunc, titleCh <-chan string) *SwayTracker {
	t := &SwayTracker{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go t.watch(ctx, titleCh)
	return t
}

func (t *SwayTracker) watch(ctx context.Context, titleCh <-chan string) {
	defer close(t.done)
	for {
		select {
		case <-ctx.Done():
			return
		case title, ok := <-titleCh:
			if !ok {
				return
			}
			t.mu.Lock()
			t.currentTitle = title
			t.mu.Unlock()
		}
	}
}

// IsFocused returns true if the cached focused container name contains
// targetTitle (case-insensitive). Returns ErrClosed after Close has been
// called.
func (t *SwayTracker) IsFocused(targetTitle string) (bool, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.closed {
		return false, ErrClosed
	}
	return strings.Contains(
		strings.ToLower(t.currentTitle),
		strings.ToLower(targetTitle),
	), nil
}

// Close cancels the background goroutine and blocks until it exits.
// Subsequent calls to IsFocused return ErrClosed.
func (t *SwayTracker) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	t.cancel()
	<-t.done
	return nil
}

// startSwaySpy starts swaymsg -t subscribe -m '["window"]' and returns a
// channel that receives the focused container name whenever a focus event is
// seen. The channel is closed when the swaymsg process exits or ctx is
// cancelled.
func startSwaySpy(ctx context.Context) (<-chan string, error) {
	cmd := exec.CommandContext(ctx, "swaymsg", "-t", "subscribe", "-m", `["window"]`)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("swaymsg pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start swaymsg: %w", err)
	}

	ch := make(chan string, 8)
	go func() {
		defer close(ch)
		defer cmd.Wait() //nolint:errcheck
		parseSwaymsgOutput(ctx, stdout, ch)
	}()

	return ch, nil
}

// parseSwaymsgOutput reads JSON events from r and sends the container name of
// each "focus" change event to ch. It returns when r reaches EOF, r errors,
// or ctx is cancelled.
func parseSwaymsgOutput(ctx context.Context, r io.Reader, ch chan<- string) {
	decoder := json.NewDecoder(r)
	for {
		if ctx.Err() != nil {
			return
		}
		var ev swayEvent
		if err := decoder.Decode(&ev); err != nil {
			if err != io.EOF {
				slog.Debug("sway: JSON decode error", "err", err)
			}
			return
		}
		if !strings.EqualFold(ev.Change, "focus") || ev.Container.Name == "" {
			continue
		}
		select {
		case ch <- ev.Container.Name:
		case <-ctx.Done():
			return
		}
	}
}
