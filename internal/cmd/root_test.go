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

	done := make(chan bool, 1)
	go func() { done <- confirm("delete?") }()
	select {
	case got := <-done:
		if got {
			t.Error("confirm with /dev/null stdin = true, want false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("confirm blocked on /dev/null stdin (misread as a terminal)")
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
		if got := confirm("delete?"); got != tt.want {
			t.Errorf("confirm with piped %q = %v, want %v", tt.line, got, tt.want)
		}
		r.Close()
	}
}
