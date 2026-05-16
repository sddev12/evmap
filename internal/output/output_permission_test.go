package output

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestOUT06_OpenPermissionDeniedMessage codifies OUT-06.
func TestOUT06_OpenPermissionDeniedMessage(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{name: "eacces", err: unix.EACCES},
		{name: "eperm", err: unix.EPERM},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			originalOpen := unixOpen
			unixOpen = func(_ string, _ int, _ uint32) (int, error) {
				return -1, tc.err
			}
			t.Cleanup(func() {
				unixOpen = originalOpen
			})

			_, err := Open()
			if err == nil {
				t.Fatalf("expected error")
			}

			if !errors.Is(err, tc.err) {
				t.Fatalf("expected wrapped error %v, got %v", tc.err, err)
			}

			if !strings.Contains(err.Error(), "open /dev/uinput") {
				t.Fatalf("expected open context in error, got %q", err.Error())
			}

			expected := "permission denied on /dev/uinput — add yourself to the 'uinput' group and ensure the udev rule is in place"
			if !strings.Contains(err.Error(), expected) {
				t.Fatalf("expected permission guidance %q in error, got %q", expected, err.Error())
			}
		})
	}
}
