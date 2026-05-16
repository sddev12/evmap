package focus

import (
	"context"
	"errors"
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

	_, err := New()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnimplemented) {
		t.Fatalf("expected ErrUnimplemented, got %v", err)
	}
	if !strings.Contains(err.Error(), "kwin") {
		t.Fatalf("expected error to mention kwin, got %q", err.Error())
	}
}

// TestFOC10_KWinDetectionCaseInsensitive ensures XDG_CURRENT_DESKTOP matching
// is case-insensitive (e.g. "kde" or "KDE Plasma" both match).
func TestFOC10_KWinDetectionCaseInsensitive(t *testing.T) {
	for _, desktop := range []string{"kde", "KDE Plasma", "KDE:KDE"} {
		t.Run(desktop, func(t *testing.T) {
			t.Setenv("WAYLAND_DISPLAY", "wayland-0")
			t.Setenv("XDG_CURRENT_DESKTOP", desktop)
			t.Setenv("SWAYSOCK", "")
			t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")

			_, err := New()
			if !errors.Is(err, ErrUnimplemented) {
				t.Fatalf("XDG_CURRENT_DESKTOP=%q: expected ErrUnimplemented, got %v", desktop, err)
			}
		})
	}
}

// waitForFocus polls IsFocused until it returns true or the deadline is exceeded.
func waitForFocus(tracker *X11Tracker, title string, timeout time.Duration) bool {
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
