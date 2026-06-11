package auth

import (
	"fmt"
	"os"
)

// ttyLine writes one line to stderr, resetting column 0 (safe after a TUI).
func ttyLine(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\r"+format+"\r\n", args...)
}

func printDeviceCodePrompt(url, code string) {
	ttyLine("")
	ttyLine("Open this URL in any browser:")
	ttyLine("  %s", url)
	ttyLine("")
	ttyLine("Enter this code:")
	ttyLine("  %s", code)
	ttyLine("")
	ttyLine("Waiting for authorization…")
}
