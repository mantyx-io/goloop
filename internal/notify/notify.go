package notify

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Send posts a best-effort desktop notification. Failures are silently
// ignored — notifications are a convenience, never a dependency.
func Send(title, message string) {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q", message, title)
		_ = exec.Command("osascript", "-e", script).Run()
	case "linux":
		if _, err := exec.LookPath("notify-send"); err == nil {
			_ = exec.Command("notify-send", title, message).Run()
		}
	}
}
