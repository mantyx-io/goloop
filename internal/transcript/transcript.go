package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger appends run events to a JSONL file under .goloop/logs/. It is
// best-effort: New returns nil on any error and every method is nil-safe,
// so callers never guard or fail on transcript problems.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

func New(dir string) *Logger {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	name := "run-" + time.Now().Format("20060102-150405") + ".jsonl"
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return &Logger{file: file, enc: json.NewEncoder(file)}
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.file.Name()
}

// Log writes one event line. Payload keys are merged next to ts/event/iteration.
func (l *Logger) Log(event string, iteration int, payload map[string]any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339),
		"event": event,
	}
	if iteration > 0 {
		entry["iteration"] = iteration
	}
	for k, v := range payload {
		entry[k] = v
	}
	_ = l.enc.Encode(entry)
}

func (l *Logger) Close() {
	if l == nil {
		return
	}
	_ = l.file.Close()
}
