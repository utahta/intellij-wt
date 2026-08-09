// Package idea opens and focuses IntelliJ IDEA project windows.
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
// own project-open path, which handles the focusing; on macOS `open -a`
// (which only activates the app) serves as a fallback.
func Open(path string) error {
	if launcher := findLauncher(); launcher != "" {
		cmd := exec.Command(launcher, path)
		if err := cmd.Start(); err == nil {
			// Don't wait: with no running instance the launcher stays
			// attached to the IDE process it starts.
			go func() { _ = cmd.Wait() }()
			activate()
			return nil
		}
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-a", appName, path).Run()
	}
	return fmt.Errorf("no IntelliJ IDEA launcher found: install the idea command-line launcher or set IWT_IDEA_BIN")
}

// activate raises the IDE application itself. Opening a project does not
// always bring it forward — a modal dialog (e.g. the project trust
// prompt) can leave IDEA hidden behind the terminal, looking like
// nothing happened. Best-effort.
func activate() {
	if runtime.GOOS != "darwin" {
		return
	}
	_ = exec.Command("open", "-a", appName).Run()
}

// findLauncher locates the "idea" command-line launcher, in order:
// $IWT_IDEA_BIN, PATH, the JetBrains Toolbox scripts directories (macOS
// and Linux), and the macOS app bundle (whose binary also forwards to a
// running instance). Candidates that don't exist are skipped, so the
// platform-foreign paths are harmless.
func findLauncher() string {
	candidates := []string{os.Getenv("IWT_IDEA_BIN")}
	if p, err := exec.LookPath("idea"); err == nil {
		candidates = append(candidates, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "Library", "Application Support", "JetBrains", "Toolbox", "scripts", "idea"),
			filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "scripts", "idea"))
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
