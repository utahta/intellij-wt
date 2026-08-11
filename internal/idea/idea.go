// Package idea opens and focuses IntelliJ IDEA project windows.
package idea

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
			activate(launcher)
			return nil
		}
	}
	if runtime.GOOS == "darwin" {
		app := os.Getenv("IWT_IDEA_APP")
		if app == "" {
			app = appName
		}
		return exec.Command("open", "-a", app, path).Run()
	}
	return fmt.Errorf("no IntelliJ IDEA launcher found: install the idea command-line launcher or set IWT_IDEA_BIN")
}

// activate raises the IDE application itself. Opening a project does not
// always bring it forward — a modal dialog (e.g. the project trust
// prompt) can leave IDEA hidden behind the terminal, looking like
// nothing happened. It targets the app the launcher belongs to: naming
// an edition instead could raise — or even start — a different one (CE,
// EAP, Toolbox installs). When no app can be determined the project is
// open already, so no activation beats a wrong one. Best-effort.
func activate(launcher string) {
	if runtime.GOOS != "darwin" {
		return
	}
	if app := activationTarget(launcher); app != "" {
		_ = exec.Command("open", "-a", app).Run()
	}
}

// activationTarget picks the app to activate: $IWT_IDEA_APP names it
// explicitly (a bundle path or an app name, for script launchers whose
// bundle iwt does not guess), else the launcher's own bundle, else
// nothing.
func activationTarget(launcher string) string {
	if app := os.Getenv("IWT_IDEA_APP"); app != "" {
		return app
	}
	return appBundle(launcher)
}

// appBundle locates the app bundle behind the launcher: the launcher
// itself (symlinks resolved) living inside one. A script launcher gets
// no guess — shell text cannot be read reliably (string concatenation,
// conditions, here-docs, …), and a skipped activation is cheap while
// activating a wrong edition is not.
func appBundle(launcher string) string {
	if p, err := filepath.EvalSymlinks(launcher); err == nil {
		launcher = p
	}
	for dir := launcher; len(dir) > 1; dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
	}
	return ""
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
