package reset

import (
	"os"
	"path/filepath"

	"github.com/mantyx-io/goloop/internal/checkpoint"
)

const outputREADME = `# Output Project

Worker-built artifacts live here. Loop infrastructure stays at the project root.

Reset this sandbox:

` + "```bash\n" + `goloop --reset --dry-run
` + "```\n"

func State(checkpointPath, userContextPath, outputDir, goal string) ([]string, error) {
	if err := os.Remove(userContextPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	removed, err := ClearOutputDir(outputDir)
	if err != nil {
		return nil, err
	}

	ckpt := checkpoint.New(checkpointPath, goal)
	if err := ckpt.WriteInitial(); err != nil {
		return nil, err
	}
	return removed, nil
}

func ClearOutputDir(outputDir string) ([]string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, err
	}

	var removed []string
	for _, entry := range entries {
		if entry.Name() == ".gitkeep" {
			continue
		}
		path := filepath.Join(outputDir, entry.Name())
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
			if err := os.RemoveAll(path); err != nil {
				return removed, err
			}
		} else {
			if err := os.Remove(path); err != nil {
				return removed, err
			}
		}
		removed = append(removed, name)
	}

	if err := os.WriteFile(filepath.Join(outputDir, ".gitkeep"), nil, 0o644); err != nil {
		return removed, err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "README.md"), []byte(outputREADME), 0o644); err != nil {
		return removed, err
	}
	return removed, nil
}
