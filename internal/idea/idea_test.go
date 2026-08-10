package idea

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppBundleInsidePath(t *testing.T) {
	got := appBundle("/Applications/IntelliJ IDEA CE.app/Contents/MacOS/idea")
	if want := "/Applications/IntelliJ IDEA CE.app"; got != want {
		t.Errorf("appBundle = %q, want %q", got, want)
	}
}

func TestAppBundleThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "X.app", "Contents", "MacOS", "idea")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "idea-link")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	// t.TempDir may itself sit behind a symlink (macOS /var), so compare
	// resolved paths.
	want, err := filepath.EvalSymlinks(filepath.Join(dir, "X.app"))
	if err != nil {
		t.Fatal(err)
	}
	if got := appBundle(link); got != want {
		t.Errorf("appBundle = %q, want %q", got, want)
	}
}

func TestAppBundleNeverGuessesFromScripts(t *testing.T) {
	// Script launchers are deliberately not parsed, however unambiguous
	// they look: shell text cannot be read reliably, and activating a
	// wrong edition is worse than skipping the activation.
	dir := t.TempDir()
	script := filepath.Join(dir, "idea")
	content := "#!/bin/sh\nexec \"/Applications/IntelliJ IDEA.app/Contents/MacOS/idea\" \"$@\"\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := appBundle(script); got != "" {
		t.Errorf("appBundle on a script = %q, want empty", got)
	}
}

func TestActivationTarget(t *testing.T) {
	launcher := "/Applications/IntelliJ IDEA CE.app/Contents/MacOS/idea"

	// IWT_IDEA_APP wins over the launcher-derived bundle: it exists for
	// setups (script launchers) where nothing can be derived, and an
	// explicit choice beats a derived one everywhere else.
	t.Setenv("IWT_IDEA_APP", "/Users/u/Applications/IntelliJ IDEA Ultimate.app")
	if got, want := activationTarget(launcher), "/Users/u/Applications/IntelliJ IDEA Ultimate.app"; got != want {
		t.Errorf("activationTarget with IWT_IDEA_APP = %q, want %q", got, want)
	}

	t.Setenv("IWT_IDEA_APP", "")
	if got, want := activationTarget(launcher), "/Applications/IntelliJ IDEA CE.app"; got != want {
		t.Errorf("activationTarget without IWT_IDEA_APP = %q, want %q", got, want)
	}
}
