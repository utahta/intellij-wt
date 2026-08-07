// Package idea opens IntelliJ IDEA project windows on macOS.
package idea

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const appName = "IntelliJ IDEA"

// Open opens path as a project in IDEA. When the project is already open,
// IDEA itself raises and focuses the existing window. The "idea"
// command-line launcher is preferred because it routes through the IDE's
// own project-open path, which handles the focusing; `open -a` (the
// fallback) only activates the app.
func Open(path string) error {
	if err := ensureDarwin(); err != nil {
		return err
	}
	if launcher := findLauncher(); launcher != "" {
		cmd := exec.Command(launcher, path)
		if err := cmd.Start(); err == nil {
			// Don't wait: with no running instance the launcher stays
			// attached to the IDE process it starts.
			go func() { _ = cmd.Wait() }()
			return nil
		}
	}
	return exec.Command("open", "-a", appName, path).Run()
}

// findLauncher locates the "idea" command-line launcher, in order:
// $IWT_IDEA_BIN, PATH, the JetBrains Toolbox scripts directory, and the
// app bundle (whose binary also forwards to a running instance).
func findLauncher() string {
	candidates := []string{os.Getenv("IWT_IDEA_BIN")}
	if p, err := exec.LookPath("idea"); err == nil {
		candidates = append(candidates, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "Library", "Application Support", "JetBrains", "Toolbox", "scripts", "idea"))
	}
	candidates = append(candidates, "/Applications/IntelliJ IDEA.app/Contents/MacOS/idea")
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func ensureDarwin() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("IntelliJ IDEA integration supports macOS only")
	}
	return nil
}
