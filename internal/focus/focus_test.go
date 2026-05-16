package focus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestFOC02_IsFocusedReturnsTrueWhenFocused codifies FOC-02.
func TestFOC02_IsFocusedReturnsTrueWhenFocused(t *testing.T) {
	titleCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newX11Tracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "Hearts of Iron IV"

	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("expected IsFocused to return true within 150 ms")
	}
}

// TestFOC03_IsFocusedReturnsFalseAfterFocusLost codifies FOC-03.
func TestFOC03_IsFocusedReturnsFalseAfterFocusLost(t *testing.T) {
	titleCh := make(chan string, 2)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newX11Tracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "Hearts of Iron IV"
	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("tracker did not pick up initial focus")
	}

	titleCh <- "Firefox"

	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		focused, err := tracker.IsFocused("Hearts of Iron IV")
		if err != nil {
			t.Fatalf("IsFocused: %v", err)
		}
		if !focused {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected IsFocused to return false after focus moved to another window")
}

// TestFOC04_FocusChangeDetectedWithin150ms codifies FOC-04.
func TestFOC04_FocusChangeDetectedWithin150ms(t *testing.T) {
	titleCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newX11Tracker(ctx, cancel, titleCh)
	defer tracker.Close()

	start := time.Now()
	titleCh <- "Hearts of Iron IV"

	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatalf("focus change not detected within 150 ms (elapsed: %v)", time.Since(start))
	}
}

// TestFOC05_X11BackendSelectedWhenDisplaySet codifies FOC-05.
func TestFOC05_X11BackendSelectedWhenDisplaySet(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("SWAYSOCK", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	t.Setenv("DISPLAY", ":0")

	orig := startSpy
	t.Cleanup(func() { startSpy = orig })
	startSpy = func(_ context.Context) (<-chan string, error) {
		return make(chan string), nil
	}

	tracker, err := New()
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	defer tracker.Close()

	if _, ok := tracker.(*X11Tracker); !ok {
		t.Fatalf("expected *X11Tracker, got %T", tracker)
	}
}

// TestFOC05_X11BackendSelectedAsXWaylandFallback codifies the XWayland
// fallback part of FOC-05: WAYLAND_DISPLAY is set but no specific compositor
// matches, so New falls through to X11Tracker when DISPLAY is also set.
func TestFOC05_X11BackendSelectedAsXWaylandFallback(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("SWAYSOCK", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	t.Setenv("DISPLAY", ":0")

	orig := startSpy
	t.Cleanup(func() { startSpy = orig })
	startSpy = func(_ context.Context) (<-chan string, error) {
		return make(chan string), nil
	}

	tracker, err := New()
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	defer tracker.Close()

	if _, ok := tracker.(*X11Tracker); !ok {
		t.Fatalf("expected *X11Tracker (XWayland fallback), got %T", tracker)
	}
}

// TestFOC07_ErrNoDisplayWhenNoDisplayEnvVars codifies FOC-07.
func TestFOC07_ErrNoDisplayWhenNoDisplayEnvVars(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	_, err := New()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNoDisplay) {
		t.Fatalf("expected ErrNoDisplay, got %v", err)
	}
}

// TestFOC08_IsFocusedCaseInsensitive codifies FOC-08.
func TestFOC08_IsFocusedCaseInsensitive(t *testing.T) {
	titleCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newX11Tracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "hearts of iron iv"

	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("expected case-insensitive match to return true")
	}
}

// TestFOC09_CloseReleasesResourcesAndSubsequentIsFocusedReturnsError codifies FOC-09.
func TestFOC09_CloseReleasesResourcesAndSubsequentIsFocusedReturnsError(t *testing.T) {
	titleCh := make(chan string)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newX11Tracker(ctx, cancel, titleCh)

	if err := tracker.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	_, err := tracker.IsFocused("anything")
	if err == nil {
		t.Fatal("expected error after Close, got nil")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// TestFOC10_KWinBackendSelectedOnKDEPlasmaWayland codifies FOC-10.
func TestFOC10_KWinBackendSelectedOnKDEPlasmaWayland(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	t.Setenv("SWAYSOCK", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	t.Setenv("DISPLAY", "")

	origLookPath := lookPath
	origPoller := startKWinPoller
	t.Cleanup(func() {
		lookPath = origLookPath
		startKWinPoller = origPoller
	})
	lookPath = func(file string) (string, error) {
		return "/usr/bin/" + file, nil
	}
	startKWinPoller = func(_ context.Context, _ string) (<-chan string, error) {
		return make(chan string), nil
	}

	tracker, err := New()
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	defer tracker.Close()

	if _, ok := tracker.(*KWinTracker); !ok {
		t.Fatalf("expected *KWinTracker, got %T", tracker)
	}
}

// TestFOC10_KWinDetectionCaseInsensitive ensures XDG_CURRENT_DESKTOP matching
// is case-insensitive (e.g. "kde" or "KDE Plasma" both match).
func TestFOC10_KWinDetectionCaseInsensitive(t *testing.T) {
	origLookPath := lookPath
	origPoller := startKWinPoller
	t.Cleanup(func() {
		lookPath = origLookPath
		startKWinPoller = origPoller
	})
	lookPath = func(file string) (string, error) {
		return "/usr/bin/" + file, nil
	}
	startKWinPoller = func(_ context.Context, _ string) (<-chan string, error) {
		return make(chan string), nil
	}

	for _, desktop := range []string{"kde", "KDE Plasma", "KDE:KDE"} {
		t.Run(desktop, func(t *testing.T) {
			t.Setenv("WAYLAND_DISPLAY", "wayland-0")
			t.Setenv("XDG_CURRENT_DESKTOP", desktop)
			t.Setenv("SWAYSOCK", "")
			t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
			t.Setenv("DISPLAY", "")

			tracker, err := New()
			if err != nil {
				t.Fatalf("XDG_CURRENT_DESKTOP=%q: New() error = %v", desktop, err)
			}
			defer tracker.Close()

			if _, ok := tracker.(*KWinTracker); !ok {
				t.Fatalf("XDG_CURRENT_DESKTOP=%q: expected *KWinTracker, got %T", desktop, tracker)
			}
		})
	}
}

// TestFOC02_KWinIsFocusedReturnsTrueWhenFocused codifies FOC-02 for KWinTracker.
func TestFOC02_KWinIsFocusedReturnsTrueWhenFocused(t *testing.T) {
	titleCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newKWinTracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "Hearts of Iron IV"

	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("expected IsFocused to return true within 150 ms")
	}
}

// TestFOC03_KWinIsFocusedReturnsFalseAfterFocusLost codifies FOC-03 for KWinTracker.
func TestFOC03_KWinIsFocusedReturnsFalseAfterFocusLost(t *testing.T) {
	titleCh := make(chan string, 2)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newKWinTracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "Hearts of Iron IV"
	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("tracker did not pick up initial focus")
	}

	titleCh <- "Firefox"

	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		focused, err := tracker.IsFocused("Hearts of Iron IV")
		if err != nil {
			t.Fatalf("IsFocused: %v", err)
		}
		if !focused {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected IsFocused to return false after focus moved to another window")
}

// TestFOC04_KWinFocusChangeDetectedWithin150ms codifies FOC-04 for KWinTracker.
func TestFOC04_KWinFocusChangeDetectedWithin150ms(t *testing.T) {
	titleCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newKWinTracker(ctx, cancel, titleCh)
	defer tracker.Close()

	start := time.Now()
	titleCh <- "Hearts of Iron IV"

	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatalf("focus change not detected within 150 ms (elapsed: %v)", time.Since(start))
	}
}

// TestFOC09_KWinCloseReleasesResourcesAndSubsequentIsFocusedReturnsError codifies FOC-09 for KWinTracker.
func TestFOC09_KWinCloseReleasesResourcesAndSubsequentIsFocusedReturnsError(t *testing.T) {
	titleCh := make(chan string)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newKWinTracker(ctx, cancel, titleCh)

	if err := tracker.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	_, err := tracker.IsFocused("anything")
	if err == nil {
		t.Fatal("expected error after Close, got nil")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// TestNewKWinTrackerReturnsHelpfulErrorWhenQDBusMissing ensures KDE Wayland
// sessions surface an install hint when neither qdbus6 nor qdbus is available.
func TestNewKWinTrackerReturnsHelpfulErrorWhenQDBusMissing(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	t.Setenv("SWAYSOCK", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	t.Setenv("DISPLAY", "")

	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(file string) (string, error) {
		return "", errors.New("not found")
	}

	_, err := New()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "qt6-tools-qdbus") {
		t.Fatalf("expected qdbus install hint, got %q", err.Error())
	}
}

// TestNewKWinTrackerReturnsHelpfulErrorWhenXdotoolMissing ensures KDE Wayland
// sessions surface an xdotool install hint when qdbus is available but xdotool is not.
func TestNewKWinTrackerReturnsHelpfulErrorWhenXdotoolMissing(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	t.Setenv("SWAYSOCK", "")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	t.Setenv("DISPLAY", "")

	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(file string) (string, error) {
		if file == "qdbus6" {
			return "/usr/bin/qdbus6", nil
		}
		return "", errors.New("not found")
	}

	_, err := New()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sudo apt install xdotool") {
		t.Fatalf("expected xdotool install hint, got %q", err.Error())
	}
}

func TestFindQDBusCommandFallsBackToQdbus(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(file string) (string, error) {
		if file == "qdbus" {
			return "/usr/bin/qdbus", nil
		}
		return "", errors.New("not found")
	}

	command, err := findQDBusCommand()
	if err != nil {
		t.Fatalf("findQDBusCommand() returned error: %v", err)
	}
	if command != "qdbus" {
		t.Fatalf("expected qdbus fallback, got %q", command)
	}
}

// waitForFocus polls IsFocused until it returns true or the deadline is exceeded.
func waitForFocus(tracker Tracker, title string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		focused, err := tracker.IsFocused(title)
		if err != nil {
			return false
		}
		if focused {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// TestFOC06_SwayBackendSelectedWhenSwaySocketSet codifies FOC-06.
func TestFOC06_SwayBackendSelectedWhenSwaySocketSet(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("SWAYSOCK", "/run/user/1000/sway-ipc.sock")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	t.Setenv("DISPLAY", "")

	orig := startSwaySubscribe
	t.Cleanup(func() { startSwaySubscribe = orig })
	startSwaySubscribe = func(_ context.Context) (<-chan string, error) {
		return make(chan string), nil
	}

	tracker, err := New()
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	defer tracker.Close()

	if _, ok := tracker.(*SwayTracker); !ok {
		t.Fatalf("expected *SwayTracker, got %T", tracker)
	}
}

// TestFOC06_SwayTrackerIsFocusedReturnsTrueWhenFocused tests that SwayTracker
// returns true when the current title matches.
func TestFOC06_SwayTrackerIsFocusedReturnsTrueWhenFocused(t *testing.T) {
	titleCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newSwayTracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "Hearts of Iron IV"

	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("expected SwayTracker.IsFocused to return true within 150 ms")
	}
}

// TestFOC06_SwayTrackerIsFocusedCaseInsensitive tests that SwayTracker
// title matching is case-insensitive (FOC-08 applied to Sway backend).
func TestFOC06_SwayTrackerIsFocusedCaseInsensitive(t *testing.T) {
	titleCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newSwayTracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "hearts of iron iv"

	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("expected case-insensitive match to return true for SwayTracker")
	}
}

// TestFOC06_SwayTrackerCloseReleasesResources tests that Close stops the
// background goroutine and subsequent IsFocused calls return ErrClosed.
func TestFOC06_SwayTrackerCloseReleasesResources(t *testing.T) {
	titleCh := make(chan string)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newSwayTracker(ctx, cancel, titleCh)

	if err := tracker.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	_, err := tracker.IsFocused("anything")
	if err == nil {
		t.Fatal("expected error after Close, got nil")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// TestFOC06_SwayTrackerParsesJSONEvents tests that startSwaySpy correctly
// parses swaymsg JSON output and extracts container names from focus events,
// ignoring non-focus events.
func TestFOC06_SwayTrackerParsesJSONEvents(t *testing.T) {
	// Build a fake swaymsg that emits a sequence of JSON events.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	ch := make(chan string, 8)
	go parseSwaymsgOutput(ctx, pr, ch)

	// Write: confirmation, non-focus event, focus event.
	events := []string{
		`{"success":true}`,
		`{"change":"title","container":{"name":"ignored"}}`,
		`{"change":"focus","container":{"name":"Hearts of Iron IV"}}`,
	}
	for _, ev := range events {
		if _, err := fmt.Fprintln(pw, ev); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	pw.Close()

	select {
	case title := <-ch:
		if title != "Hearts of Iron IV" {
			t.Fatalf("expected 'Hearts of Iron IV', got %q", title)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for focus event to be parsed")
	}
}

// TestFOC11_HyprlandBackendSelectedWhenSignatureSet codifies FOC-11:
// Given WAYLAND_DISPLAY and HYPRLAND_INSTANCE_SIGNATURE are both set,
// New() must return a *HyprlandTracker.
func TestFOC11_HyprlandBackendSelectedWhenSignatureSet(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "abc123def456")
	t.Setenv("SWAYSOCK", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "")

	orig := startHyprlandWatcher
	t.Cleanup(func() { startHyprlandWatcher = orig })
	startHyprlandWatcher = func(_ context.Context) (<-chan string, error) {
		return make(chan string), nil
	}

	tracker, err := New()
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	defer tracker.Close()

	if _, ok := tracker.(*HyprlandTracker); !ok {
		t.Fatalf("expected *HyprlandTracker, got %T", tracker)
	}
}

// TestHyprlandTracker_IsFocusedReturnsTrueWhenFocused verifies that IsFocused
// returns true within 150 ms of a title arriving on the channel.
func TestHyprlandTracker_IsFocusedReturnsTrueWhenFocused(t *testing.T) {
	titleCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newHyprlandTracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "Hearts of Iron IV"

	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("expected IsFocused to return true within 150 ms")
	}
}

// TestHyprlandTracker_IsFocusedReturnsFalseAfterFocusLost verifies that
// IsFocused returns false after a different window title is received.
func TestHyprlandTracker_IsFocusedReturnsFalseAfterFocusLost(t *testing.T) {
	titleCh := make(chan string, 2)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newHyprlandTracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "Hearts of Iron IV"
	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("tracker did not pick up initial focus")
	}

	titleCh <- "Firefox"

	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		focused, err := tracker.IsFocused("Hearts of Iron IV")
		if err != nil {
			t.Fatalf("IsFocused: %v", err)
		}
		if !focused {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected IsFocused to return false after focus moved to another window")
}

// TestHyprlandTracker_IsFocusedCaseInsensitive verifies case-insensitive title
// matching for the Hyprland tracker (FOC-08 for the Hyprland backend).
func TestHyprlandTracker_IsFocusedCaseInsensitive(t *testing.T) {
	titleCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newHyprlandTracker(ctx, cancel, titleCh)
	defer tracker.Close()

	titleCh <- "hearts of iron iv"

	if !waitForFocus(tracker, "Hearts of Iron IV", 150*time.Millisecond) {
		t.Fatal("expected case-insensitive match to return true")
	}
}

// TestHyprlandTracker_CloseReleasesResources verifies that Close stops the
// background goroutine and subsequent IsFocused calls return ErrClosed (FOC-09
// for the Hyprland backend).
func TestHyprlandTracker_CloseReleasesResources(t *testing.T) {
	titleCh := make(chan string)
	ctx, cancel := context.WithCancel(context.Background())
	tracker := newHyprlandTracker(ctx, cancel, titleCh)

	if err := tracker.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	_, err := tracker.IsFocused("anything")
	if err == nil {
		t.Fatal("expected error after Close, got nil")
	}
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// TestParseHyprlandEvent verifies correct parsing of Hyprland socket event lines.
func TestParseHyprlandEvent(t *testing.T) {
	tests := []struct {
		line  string
		title string
	}{
		{"activewindow>>steam,Hearts of Iron IV", "Hearts of Iron IV"},
		{"activewindow>>firefox,Mozilla Firefox", "Mozilla Firefox"},
		{"activewindow>>,Hearts of Iron IV", "Hearts of Iron IV"},
		{"activewindow>>steam", ""},
		{"somethingelse>>steam,title", ""},
		{"", ""},
		{"activewindow>>class,Title with, commas", "Title with, commas"},
	}
	for _, tt := range tests {
		got := parseHyprlandEvent(tt.line)
		if got != tt.title {
			t.Errorf("parseHyprlandEvent(%q) = %q, want %q", tt.line, got, tt.title)
		}
	}
}
