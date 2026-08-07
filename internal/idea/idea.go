// Package idea opens and focuses IntelliJ IDEA project windows on macOS.
package idea

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const appName = "IntelliJ IDEA"

// Open opens path as a project in IDEA.
func Open(path string) error {
	if err := ensureDarwin(); err != nil {
		return err
	}
	return exec.Command("open", "-a", appName, path).Run()
}

// OpenOrFocus raises the project window whose title starts with the
// directory name of path. IDEA titles its windows "<project> – <file>", so a
// prefix match identifies the project window. When no window matches (the
// project is not open, or the Accessibility permission is missing), it falls
// back to opening the path as a project.
func OpenOrFocus(path string) error {
	if err := ensureDarwin(); err != nil {
		return err
	}
	if err := focus(filepath.Base(path)); err != nil {
		return Open(path)
	}
	return nil
}

func focus(project string) error {
	name := strings.ReplaceAll(project, `"`, `\"`)
	script := fmt.Sprintf(`tell application "System Events"
  tell (first application process whose bundle identifier is "com.jetbrains.intellij")
    perform action "AXRaise" of (first window whose (name is "%[1]s" or name begins with "%[1]s "))
    set frontmost to true
  end tell
end tell`, name)
	return exec.Command("osascript", "-e", script).Run()
}

func ensureDarwin() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("IntelliJ IDEA integration supports macOS only")
	}
	return nil
}
