package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigureOptions updates config files (global or per-project).
type ConfigureOptions struct {
	Global            bool
	ConfigPath        string
	ProjectRoot       string
	Objective         string
	OutputDir         string
	SupervisorBackend string
	SupervisorModel   string
	WorkerBackend     string
	CursorModel       string
	ClaudeCodeModel   string
	MaxIterations     int
	Interactive       *bool
}

// InitOptions initializes a new project (.goloop/config.yaml).
type InitOptions struct {
	ProjectRoot   string
	ConfigPath    string
	Objective     string
	OutputDir     string
	MaxIterations int
	Interactive   *bool
}

// Snapshot is a read-only view of merged settings.
type Snapshot struct {
	Objective         string
	Goal              string
	OutputDir         string
	SupervisorBackend string
	SupervisorModel   string
	WorkerBackend     string
	CursorModel       string
	ClaudeCodeModel   string
	MaxIterations     int
	Interactive       bool
}

func LoadSnapshot(path string) (*Snapshot, error) {
	raw, err := LoadRaw(path)
	if err != nil {
		return nil, err
	}
	return raw.snapshot(), nil
}

func LoadMergedSnapshot(projectRoot string) (*Snapshot, error) {
	raw, _, err := LoadMerged(projectRoot, "")
	if err != nil {
		return nil, err
	}
	return raw.snapshot(), nil
}

func (r *rawConfig) snapshot() *Snapshot {
	interactive := true
	if r.Loop.Interactive != nil {
		interactive = *r.Loop.Interactive
	}
	return &Snapshot{
		Objective:         r.Objective,
		Goal:              r.Goal,
		OutputDir:         r.Paths.OutputDir,
		SupervisorBackend: r.Supervisor.Backend,
		SupervisorModel:   r.Supervisor.Model,
		WorkerBackend:     r.Worker.Backend,
		CursorModel:       r.Cursor.Model,
		ClaudeCodeModel:   r.ClaudeCode.Model,
		MaxIterations:     r.Loop.MaxIterations,
		Interactive:       interactive,
	}
}

// ResolveConfigPath returns the config file path for configure (local or global).
func ResolveConfigPath(root string, explicit string, global bool) string {
	if global {
		if explicit != "" {
			return ExpandHome(explicit)
		}
		return GlobalConfigPath()
	}
	if explicit != "" {
		if filepath.IsAbs(explicit) {
			return explicit
		}
		return filepath.Join(root, explicit)
	}
	return LocalConfigPath(root)
}

func LoadRaw(path string) (*rawConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultRaw(), nil
	}
	if err != nil {
		return nil, err
	}
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &raw, nil
}

func Save(path string, raw *rawConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	header := "# Goloop configuration — https://github.com/mantyx-io/goloop\n"
	return os.WriteFile(path, append([]byte(header), data...), 0o644)
}

func Configure(opts ConfigureOptions) (string, error) {
	if opts.Global {
		return configureGlobal(opts)
	}
	return configureLocal(opts)
}

// Init creates the project config file.
func Init(opts InitOptions) (string, error) {
	return Configure(ConfigureOptions{
		ConfigPath:    opts.ConfigPath,
		ProjectRoot:   opts.ProjectRoot,
		Objective:     opts.Objective,
		OutputDir:     opts.OutputDir,
		MaxIterations: opts.MaxIterations,
		Interactive:   opts.Interactive,
	})
}

func configureGlobal(opts ConfigureOptions) (string, error) {
	path := ResolveConfigPath("", opts.ConfigPath, true)
	raw, err := LoadRaw(path)
	if err != nil {
		return "", err
	}
	applyConfigurePatch(raw, opts, true)
	applyDefaults(raw)
	raw.Objective = ""
	raw.Goal = ""
	// Paths are per-project; keep output_dir default only in global as hint
	raw.Paths.Checkpoint = ""
	raw.Paths.UserContext = ""
	if err := Save(path, raw); err != nil {
		return "", err
	}
	return path, nil
}

func configureLocal(opts ConfigureOptions) (string, error) {
	root, err := filepath.Abs(opts.ProjectRoot)
	if err != nil {
		return "", err
	}
	path := ResolveConfigPath(root, opts.ConfigPath, false)

	raw := &rawConfig{}
	if opts.Objective != "" {
		raw.Objective = strings.TrimSpace(opts.Objective)
	}
	applyConfigurePatch(raw, opts, false)

	if strings.TrimSpace(raw.Objective) == "" && strings.TrimSpace(raw.Goal) == "" {
		return "", fmt.Errorf("objective is required for this project")
	}

	if err := os.MkdirAll(ProjectGoloopDir(root), 0o755); err != nil {
		return "", err
	}
	if err := Save(path, raw); err != nil {
		return "", err
	}
	return path, nil
}

func applyConfigurePatch(raw *rawConfig, opts ConfigureOptions, global bool) {
	if opts.Objective != "" && !global {
		raw.Objective = strings.TrimSpace(opts.Objective)
		raw.Goal = ""
	}
	if opts.SupervisorBackend != "" {
		raw.Supervisor.Backend = opts.SupervisorBackend
	}
	if opts.SupervisorModel != "" {
		raw.Supervisor.Model = opts.SupervisorModel
	}
	if opts.WorkerBackend != "" {
		raw.Worker.Backend = opts.WorkerBackend
	}
	if opts.CursorModel != "" {
		raw.Cursor.Model = opts.CursorModel
	}
	if opts.ClaudeCodeModel != "" {
		raw.ClaudeCode.Model = opts.ClaudeCodeModel
	}
	if opts.MaxIterations > 0 {
		raw.Loop.MaxIterations = opts.MaxIterations
	}
	if opts.Interactive != nil {
		raw.Loop.Interactive = opts.Interactive
	}
	if opts.OutputDir != "" && !global {
		raw.Paths.OutputDir = strings.TrimSpace(opts.OutputDir)
	}
}

func defaultRaw() *rawConfig {
	showReasoning := true
	interactive := true
	return &rawConfig{
		Supervisor: supervisorYAML{
			Backend:     string(SupervisorChatGPT),
			Model:       "gpt-4.1",
			Temperature: 0.3,
		},
		Worker: workerYAML{
			Backend:        string(WorkerCursor),
			BuilderAgent:   "builder",
			EvaluatorAgent: "evaluator",
			ToolsmithAgent: "toolsmith",
			TimeoutSeconds: 1800,
			ShowReasoning:  &showReasoning,
		},
		Cursor: cursorYAML{
			Binary: "cursor",
			Model:  "composer-2.5-fast",
		},
		ClaudeCode: claudeCodeYAML{
			Binary: "claude",
			Model:  "claude-sonnet-4-6",
		},
		Tools: toolsYAML{
			RestartExitCode: 75,
		},
		Loop: loopYAML{
			MaxIterations: 50,
			PauseSeconds:  2,
			Interactive:   &interactive,
		},
		Paths: pathsYAML{
			OutputDir: ".",
		},
	}
}

func applyDefaults(raw *rawConfig) {
	if raw.Supervisor.Backend == "" {
		raw.Supervisor.Backend = string(SupervisorChatGPT)
	}
	if raw.Supervisor.Model == "" {
		raw.Supervisor.Model = "gpt-4.1"
	}
	if raw.Supervisor.Temperature == 0 {
		raw.Supervisor.Temperature = 0.3
	}
	if raw.Worker.Backend == "" {
		raw.Worker.Backend = string(WorkerCursor)
	}
	if raw.Worker.BuilderAgent == "" {
		raw.Worker.BuilderAgent = "builder"
	}
	if raw.Worker.EvaluatorAgent == "" {
		raw.Worker.EvaluatorAgent = "evaluator"
	}
	if raw.Worker.ToolsmithAgent == "" {
		raw.Worker.ToolsmithAgent = "toolsmith"
	}
	if raw.Worker.TimeoutSeconds == 0 {
		raw.Worker.TimeoutSeconds = 1800
	}
	if raw.Worker.ShowReasoning == nil {
		v := true
		raw.Worker.ShowReasoning = &v
	}
	if raw.Cursor.Binary == "" {
		raw.Cursor.Binary = "cursor"
	}
	if raw.Cursor.Model == "" {
		raw.Cursor.Model = "composer-2.5-fast"
	}
	if raw.ClaudeCode.Binary == "" {
		raw.ClaudeCode.Binary = "claude"
	}
	if raw.ClaudeCode.Model == "" {
		raw.ClaudeCode.Model = "claude-sonnet-4-6"
	}
	if raw.Tools.RestartExitCode == 0 {
		raw.Tools.RestartExitCode = 75
	}
	if raw.Loop.MaxIterations == 0 {
		raw.Loop.MaxIterations = 50
	}
	if raw.Loop.PauseSeconds == 0 {
		raw.Loop.PauseSeconds = 2
	}
	if raw.Loop.Interactive == nil {
		v := true
		raw.Loop.Interactive = &v
	}
	if raw.Paths.OutputDir == "" {
		raw.Paths.OutputDir = "."
	}
}
