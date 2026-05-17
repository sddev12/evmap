package focus

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNoDisplay is returned by New when neither DISPLAY nor WAYLAND_DISPLAY is set.
	ErrNoDisplay = errors.New("no display server detected")

	// ErrUnimplemented is returned by New for Wayland compositors whose tracker
	// has not yet been implemented (KWin).
	ErrUnimplemented = errors.New("tracker not yet implemented for this compositor")

	// ErrClosed is returned by IsFocused after Close has been called.
	ErrClosed = errors.New("tracker is closed")
)

// Tracker reports whether the target window currently has focus.
type Tracker interface {
	// IsFocused returns true if the window whose title contains targetTitle
	// (case-insensitive substring match) is the currently active window.
	// It must be safe to call concurrently.
	IsFocused(targetTitle string) (bool, error)

	// Close releases any resources held by the tracker.
	// After Close, IsFocused must return an error.
	Close() error
}

// startSpy is the function used to start the X11 spy process.
// Replaced in tests so that no real xprop process is needed.
var startSpy = startX11Spy

// New returns a Tracker appropriate for the current desktop environment.
// It detects the compositor by inspecting environment variables in this order:
//  1. WAYLAND_DISPLAY + SWAYSOCK           → SwayTracker
//  2. WAYLAND_DISPLAY + HYPRLAND_…         → HyprlandTracker
//  3. WAYLAND_DISPLAY + KDE in XDG_…       → ErrUnimplemented (kwin stub)
//  4. DISPLAY set                          → X11Tracker
//  5. neither                              → ErrNoDisplay
//
// A generic WAYLAND_DISPLAY-only session (no compositor-specific env var
// matched) falls through to step 4, which covers GNOME on Wayland and any
// other compositor where games run as XWayland clients.
func New() (Tracker, error) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if os.Getenv("SWAYSOCK") != "" {
			ctx, cancel := context.WithCancel(context.Background())
			ch, err := startSwaySubscribe(ctx)
			if err != nil {
				cancel()
				return nil, fmt.Errorf("sway tracker: %w", err)
			}
			return newSwayTracker(ctx, cancel, ch), nil
		}
		if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
			ctx, cancel := context.WithCancel(context.Background())
			ch, err := startHyprlandWatcher(ctx)
			if err != nil {
				cancel()
				return nil, fmt.Errorf("hyprland tracker: %w", err)
			}
			return newHyprlandTracker(ctx, cancel, ch), nil
		}
		if strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "KDE") {
			return nil, fmt.Errorf("kwin: %w", ErrUnimplemented)
		}
		// Generic Wayland: fall through to X11Tracker via XWayland.
	}

	if os.Getenv("DISPLAY") != "" {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := startSpy(ctx)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("x11 tracker: %w", err)
		}
		return newX11Tracker(ctx, cancel, ch), nil
	}

	return nil, ErrNoDisplay
}

// X11Tracker maintains the active X11 window title by subscribing to
// _NET_ACTIVE_WINDOW changes on the root window via xprop -spy.
type X11Tracker struct {
	mu           sync.RWMutex
	currentTitle string
	closed       bool
	cancel       context.CancelFunc
	done         chan struct{}
}

// newX11Tracker creates an X11Tracker that reads window titles from titleCh.
// cancel must cancel the context that was passed to whatever produces titleCh.
func newX11Tracker(ctx context.Context, cancel context.CancelFunc, titleCh <-chan string) *X11Tracker {
	t := &X11Tracker{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go t.watch(ctx, titleCh)
	return t
}

func (t *X11Tracker) watch(ctx context.Context, titleCh <-chan string) {
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

// IsFocused returns true if the cached active window title contains targetTitle
// (case-insensitive). Returns ErrClosed after Close has been called.
func (t *X11Tracker) IsFocused(targetTitle string) (bool, error) {
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

// Close cancels the background spy goroutine and blocks until it exits.
// Subsequent calls to IsFocused return ErrClosed.
func (t *X11Tracker) Close() error {
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

// startX11Spy starts xprop -spy -root _NET_ACTIVE_WINDOW and returns a channel
// that receives the active window title whenever focus changes. The channel is
// closed when the xprop process exits or ctx is cancelled.
func startX11Spy(ctx context.Context) (<-chan string, error) {
	cmd := exec.CommandContext(ctx, "xprop", "-spy", "-root", "_NET_ACTIVE_WINDOW")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("xprop pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start xprop: %w", err)
	}

	ch := make(chan string, 8)
	go func() {
		defer close(ch)
		defer cmd.Wait() //nolint:errcheck

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			id := parseWindowID(scanner.Text())
			if id == "" || id == "0x0" || id == "0" {
				continue
			}
			title, err := getX11WindowTitle(ctx, id)
			if err != nil {
				continue
			}
			select {
			case ch <- title:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// parseWindowID extracts the hex window ID from an xprop output line such as:
// "_NET_ACTIVE_WINDOW(WINDOW): window id # 0x3400006"
func parseWindowID(line string) string {
	const marker = "window id # "
	idx := strings.Index(line, marker)
	if idx == -1 {
		return ""
	}
	return strings.TrimSpace(line[idx+len(marker):])
}

// getX11WindowTitle returns the title of the X11 window with the given ID by
// running xprop. It tries _NET_WM_NAME (UTF-8) first, then falls back to
// the legacy WM_NAME property.
func getX11WindowTitle(ctx context.Context, windowID string) (string, error) {
	out, err := exec.CommandContext(ctx, "xprop", "-id", windowID, "_NET_WM_NAME").Output()
	if err == nil {
		if title := parseXpropString(string(out)); title != "" {
			return title, nil
		}
	}

	out, err = exec.CommandContext(ctx, "xprop", "-id", windowID, "WM_NAME").Output()
	if err != nil {
		return "", fmt.Errorf("xprop WM_NAME %s: %w", windowID, err)
	}
	title := parseXpropString(string(out))
	if title == "" {
		return "", fmt.Errorf("empty window title for %s", windowID)
	}
	return title, nil
}

// parseXpropString extracts the quoted string value from xprop output such as:
// `_NET_WM_NAME(UTF8_STRING) = "Hearts of Iron IV"`
func parseXpropString(out string) string {
	start := strings.Index(out, `"`)
	if start == -1 {
		return ""
	}
	rest := out[start+1:]
	end := strings.LastIndex(rest, `"`)
	if end == -1 {
		return ""
	}
	return rest[:end]
}

// startHyprlandWatcher is the function used to start the Hyprland event
// watcher. Replaced in tests so that no real socket or hyprctl process is
// needed.
var startHyprlandWatcher = startHyprlandWatch

// HyprlandTracker maintains the active window title by subscribing to the
// Hyprland event socket, or polling hyprctl as a fallback.
type HyprlandTracker struct {
	mu           sync.RWMutex
	currentTitle string
	closed       bool
	cancel       context.CancelFunc
	done         chan struct{}
}

func newHyprlandTracker(ctx context.Context, cancel context.CancelFunc, titleCh <-chan string) *HyprlandTracker {
	t := &HyprlandTracker{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go t.watch(ctx, titleCh)
	return t
}

func (t *HyprlandTracker) watch(ctx context.Context, titleCh <-chan string) {
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

// IsFocused returns true if the cached active window title contains targetTitle
// (case-insensitive). Returns ErrClosed after Close has been called.
func (t *HyprlandTracker) IsFocused(targetTitle string) (bool, error) {
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
func (t *HyprlandTracker) Close() error {
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

// startHyprlandWatch attempts to connect to the Hyprland event socket and
// returns a channel that receives the active window title on focus changes.
// If the socket is unavailable it falls back to polling hyprctl every 100 ms.
func startHyprlandWatch(ctx context.Context) (<-chan string, error) {
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")

	ch := make(chan string, 8)
	if sig != "" && runtimeDir != "" {
		socketPath := runtimeDir + "/hypr/" + sig + "/.socket2.sock"
		if conn, err := net.Dial("unix", socketPath); err == nil {
			go readHyprlandSocket(ctx, conn, ch)
			return ch, nil
		}
	}
	// Socket unavailable: fall back to polling.
	go pollHyprlandActiveWindow(ctx, ch)
	return ch, nil
}

// readHyprlandSocket reads activewindow events from the Hyprland event socket
// and sends the active window title to ch. ch is closed when the function
// returns.
func readHyprlandSocket(ctx context.Context, conn net.Conn, ch chan<- string) {
	defer close(ch)
	defer conn.Close() //nolint:errcheck

	// Close the connection when the context is cancelled so the scanner
	// unblocks.
	go func() {
		<-ctx.Done()
		conn.Close() //nolint:errcheck
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		if title := parseHyprlandEvent(scanner.Text()); title != "" {
			select {
			case ch <- title:
			case <-ctx.Done():
				return
			}
		}
	}
}

// parseHyprlandEvent parses a line from the Hyprland event socket.
// activewindow events have the format "activewindow>>class,title".
func parseHyprlandEvent(line string) string {
	const prefix = "activewindow>>"
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	rest := line[len(prefix):]
	idx := strings.Index(rest, ",")
	if idx == -1 {
		return ""
	}
	return rest[idx+1:]
}

// pollHyprlandActiveWindow polls hyprctl activewindow every 100 ms and sends
// the title to ch whenever it changes. ch is closed when the function returns.
func pollHyprlandActiveWindow(ctx context.Context, ch chan<- string) {
	defer close(ch)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	prev := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			title, err := hyprctlActiveWindow(ctx)
			if err != nil || title == prev {
				continue
			}
			prev = title
			select {
			case ch <- title:
			case <-ctx.Done():
				return
			}
		}
	}
}

// hyprctlActiveWindow runs hyprctl activewindow -j and returns the window title.
func hyprctlActiveWindow(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "hyprctl", "activewindow", "-j").Output()
	if err != nil {
		return "", fmt.Errorf("hyprctl activewindow: %w", err)
	}
	var result struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("parse hyprctl output: %w", err)
	}
	return result.Title, nil
}
