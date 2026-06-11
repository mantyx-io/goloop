package tools

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Info struct {
	Name        string
	Description string
	Path        string
}

type toolYAML struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func Describe(toolsDir string) string {
	tools, err := Discover(toolsDir)
	if err != nil || len(tools) == 0 {
		return "_(no supervisor tools installed yet)_"
	}
	var lines []string
	for _, t := range tools {
		lines = append(lines, "- **"+t.Name+"** (`"+filepath.Base(t.Path)+"`): "+t.Description)
	}
	return strings.Join(lines, "\n")
}

func Discover(toolsDir string) ([]Info, error) {
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var found []Info
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(toolsDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var spec toolYAML
		if err := yaml.Unmarshal(data, &spec); err != nil {
			continue
		}
		if spec.Name == "" {
			spec.Name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		found = append(found, Info{
			Name:        spec.Name,
			Description: strings.TrimSpace(spec.Description),
			Path:        path,
		})
	}
	return found, nil
}
