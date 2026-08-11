package cmd

import (
	"os"
	"testing"
	"time"
)

// swapStdin points os.Stdin at f for the test's duration.
func swapStdin(t *testing.T, f *os.File) {
	t.Helper()
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old })
}

// muteStderr silences the confirm prompt during the test.
func muteStderr(t *testing.T) {
	t.Helper()
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = null
	t.Cleanup(func() {
		os.Stderr = old
		null.Close()
	})
}

func TestConfirmNullStdin(t *testing.T) {
	// /dev/null is a character device but not a terminal: confirm must
	// take the line-reading path and answer No on the immediate EOF,
	// not wait for a key on /dev/tty.
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	muteStderr(t)
	swapStdin(t, null)

	type answer struct {
		yes bool
		err error
	}
	done := make(chan answer, 1)
	go func() {
		yes, err := confirm("delete?")
		done <- answer{yes, err}
	}()
	select {
	case got := <-done:
		// An answer of No, not a failure: automation that offers no
		// answer declines the step, it does not call off the run.
		if got.yes || got.err != nil {
			t.Errorf("confirm with /dev/null stdin = (%v, %v), want (false, nil)", got.yes, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("confirm blocked on /dev/null stdin (misread as a terminal)")
	}
}

func TestIsYes(t *testing.T) {
	for _, tt := range []struct {
		answer string
		want   bool
	}{
		{"y", true},
		{"Y", true},
		{"yes", true},
		{" y \n", true},
		{"n", false},
		{"", false},
		{"yep", false},
	} {
		if got := isYes(tt.answer); got != tt.want {
			t.Errorf("isYes(%q) = %v, want %v", tt.answer, got, tt.want)
		}
	}
}

func TestConfirmPipedStdin(t *testing.T) {
	muteStderr(t)
	for _, tt := range []struct {
		line string
		want bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"", false}, // immediate EOF
	} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.WriteString(tt.line); err != nil {
			t.Fatal(err)
		}
		w.Close()
		swapStdin(t, r)
		got, err := confirm("delete?")
		if err != nil {
			t.Errorf("confirm with piped %q returned %v", tt.line, err)
		}
		if got != tt.want {
			t.Errorf("confirm with piped %q = %v, want %v", tt.line, got, tt.want)
		}
		r.Close()
	}
}
