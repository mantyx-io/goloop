package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mantyx-io/goloop/internal/auth"
)

type SupervisorBackend string

const (
	SupervisorOpenAI    SupervisorBackend = "openai"
	SupervisorChatGPT   SupervisorBackend = "chatgpt"
	SupervisorAnthropic SupervisorBackend = "anthropic"
)

type WorkerBackend string

const (
	WorkerCursor     WorkerBackend = "cursor"
	WorkerClaudeCode WorkerBackend = "claude_code"
)

type Config struct {
	Goal     string
	GoalSlug string

	SupervisorBackend     SupervisorBackend
	SupervisorModel       string
	SupervisorTemperature float64
	SupervisorAPIKey      string
	SupervisorAPIKeyEnv   string
	SupervisorBaseURL     string
	SupervisorAuthPath    string

	WorkerBackend        WorkerBackend
	WorkerBuilderAgent   string
	WorkerEvaluatorAgent string
	WorkerToolsmithAgent string
	WorkerTimeoutSeconds int
	WorkerShowReasoning  bool
	WorkerAgentsDir      string
	ToolsRestartExitCode int
	ToolsDir             string

	CursorBinary     string
	CursorModel      string
	ClaudeCodeBinary string
	ClaudeCodeModel  string

	MaxIterations    int
	PauseSeconds     int
	Interactive      bool
	AdditionalPrompt string

	CheckpointPath  string
	UserContextPath string
	OutputDir       string
	ProjectRoot     string
	GoloopDir       string
	ConfigSources   []string
}

type Overrides struct {
	ConfigPath        string
	ProjectRoot       string
	Goal              string
	GoalSlug          string
	CheckpointPath    string
	UserContextPath   string
	OutputDir         string
	MaxIterations     *int
	Prompt            string
	PromptFile        string
	NoInteractive     bool
	SupervisorBackend string
	SupervisorModel   string
	WorkerBackend     string
	CursorModel       string
	ClaudeCodeModel   string
}

type rawConfig struct {
	Goal       string         `yaml:"goal"`
	Objective  string         `yaml:"objective"`
	Supervisor supervisorYAML `yaml:"supervisor"`
	Worker     workerYAML     `yaml:"worker"`
	Cursor     cursorYAML     `yaml:"cursor"`
	ClaudeCode claudeCodeYAML `yaml:"claude_code"`
	Tools      toolsYAML      `yaml:"tools"`
	Loop       loopYAML       `yaml:"loop"`
	Paths      pathsYAML      `yaml:"paths"`
}

type supervisorYAML struct {
	Backend     string  `yaml:"backend"`
	Model       string  `yaml:"model"`
	APIKeyEnv   string  `yaml:"api_key_env"`
	BaseURL     string  `yaml:"base_url"`
	Temperature float64 `yaml:"temperature"`
	AuthPath    string  `yaml:"auth_path"`
}

type workerYAML struct {
	Backend        string `yaml:"backend"`
	BuilderAgent   string `yaml:"builder_agent"`
	EvaluatorAgent string `yaml:"evaluator_agent"`
	ToolsmithAgent string `yaml:"toolsmith_agent"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	ShowReasoning  *bool  `yaml:"show_reasoning"`
	AgentsDir      string `yaml:"agents_dir"`
}

type cursorYAML struct {
	Binary string `yaml:"binary"`
	Model  string `yaml:"model"`
}

type claudeCodeYAML struct {
	Binary string `yaml:"binary"`
	Model  string `yaml:"model"`
}

type toolsYAML struct {
	RestartExitCode int    `yaml:"restart_exit_code"`
	Dir             string `yaml:"dir"`
}

type loopYAML struct {
	MaxIterations    int    `yaml:"max_iterations"`
	PauseSeconds     int    `yaml:"pause_seconds"`
	Interactive      *bool  `yaml:"interactive"`
	AdditionalPrompt string `yaml:"additional_prompt"`
}

type pathsYAML struct {
	Checkpoint  string `yaml:"checkpoint"`
	UserContext string `yaml:"user_context"`
	OutputDir   string `yaml:"output_dir"`
}

func Load(overrides Overrides) (*Config, error) {
	root, err := filepath.Abs(overrides.ProjectRoot)
	if err != nil {
		return nil, err
	}
	if root == "" {
		root, _ = os.Getwd()
	}

	raw, sources, err := LoadMerged(root, overrides.ConfigPath)
	if err != nil {
		return nil, err
	}

	backend := SupervisorBackend(raw.Supervisor.Backend)
	if backend == "" {
		backend = SupervisorChatGPT
	}
	if overrides.SupervisorBackend != "" {
		backend = SupervisorBackend(overrides.SupervisorBackend)
	}

	model := raw.Supervisor.Model
	if model == "" {
		switch backend {
		case SupervisorAnthropic:
			model = "claude-sonnet-4-6"
		case SupervisorOpenAI, SupervisorChatGPT:
			model = "gpt-4.1"
		default:
			model = "gpt-4.1"
		}
	}
	if overrides.SupervisorModel != "" {
		model = overrides.SupervisorModel
	}

	apiKeyEnv := raw.Supervisor.APIKeyEnv
	if apiKeyEnv == "" {
		switch backend {
		case SupervisorAnthropic:
			apiKeyEnv = "ANTHROPIC_API_KEY"
		default:
			apiKeyEnv = "OPENAI_API_KEY"
		}
	}
	var apiKey string
	switch backend {
	case SupervisorOpenAI, SupervisorAnthropic:
		apiKey = os.Getenv(apiKeyEnv)
		if apiKey == "" {
			if creds, err := auth.Load(auth.ResolveAuthPathForRead(raw.Supervisor.AuthPath)); err == nil {
				if backend == SupervisorAnthropic {
					apiKey = creds.AnthropicAPIKey
				} else {
					apiKey = creds.APIKey
				}
			}
		}
	}

	workerBackend := WorkerBackend(raw.Worker.Backend)
	if workerBackend == "" {
		workerBackend = WorkerCursor
	}
	if overrides.WorkerBackend != "" {
		workerBackend = WorkerBackend(overrides.WorkerBackend)
	}

	showReasoning := true
	if raw.Worker.ShowReasoning != nil {
		showReasoning = *raw.Worker.ShowReasoning
	}

	interactive := true
	if raw.Loop.Interactive != nil {
		interactive = *raw.Loop.Interactive
	}
	if overrides.NoInteractive {
		interactive = false
	}

	goloopDir := ProjectGoloopDir(root)
	checkpoint := raw.Paths.Checkpoint
	if checkpoint == "" {
		checkpoint = "checkpoint.md"
	}
	userContext := raw.Paths.UserContext
	if userContext == "" {
		userContext = "user_context.md"
	}
	outputDir := raw.Paths.OutputDir
	if outputDir == "" {
		outputDir = "."
	}

	toolsDir := raw.Tools.Dir
	if toolsDir == "" {
		toolsDir = "tools"
	}

	maxIter := raw.Loop.MaxIterations
	if maxIter == 0 {
		maxIter = 50
	}
	if overrides.MaxIterations != nil {
		maxIter = *overrides.MaxIterations
	}

	additionalPrompt := strings.TrimSpace(raw.Loop.AdditionalPrompt)
	if overrides.PromptFile != "" {
		content, err := os.ReadFile(overrides.PromptFile)
		if err != nil {
			return nil, fmt.Errorf("read prompt file: %w", err)
		}
		additionalPrompt = MergePrompts(additionalPrompt, string(content))
	}
	if overrides.Prompt != "" {
		additionalPrompt = MergePrompts(additionalPrompt, overrides.Prompt)
	}

	cursorModel := raw.Cursor.Model
	if cursorModel == "" {
		cursorModel = "composer-2.5-fast"
	}
	if overrides.CursorModel != "" {
		cursorModel = overrides.CursorModel
	}

	claudeModel := raw.ClaudeCode.Model
	if claudeModel == "" {
		claudeModel = "claude-sonnet-4-6"
	}
	if overrides.ClaudeCodeModel != "" {
		claudeModel = overrides.ClaudeCodeModel
	}

	goal := strings.TrimSpace(overrides.Goal)
	if goal == "" {
		goal = strings.TrimSpace(raw.Goal)
	}
	if goal == "" {
		goal = strings.TrimSpace(raw.Objective)
	}

	checkpointPath := filepath.Join(goloopDir, filepath.Base(checkpoint))
	if overrides.CheckpointPath != "" {
		checkpointPath = overrides.CheckpointPath
	}
	userContextPath := filepath.Join(goloopDir, filepath.Base(userContext))
	if overrides.UserContextPath != "" {
		userContextPath = overrides.UserContextPath
	}
	outputDirPath := filepath.Join(root, outputDir)
	if overrides.OutputDir != "" {
		if filepath.IsAbs(overrides.OutputDir) {
			outputDirPath = overrides.OutputDir
		} else {
			outputDirPath = filepath.Join(root, overrides.OutputDir)
		}
	}

	cfg := &Config{
		Goal:     goal,
		GoalSlug: strings.TrimSpace(overrides.GoalSlug),

		SupervisorBackend:     backend,
		SupervisorModel:       model,
		SupervisorTemperature: defaultFloat(raw.Supervisor.Temperature, 0.3),
		SupervisorAPIKey:      apiKey,
		SupervisorAPIKeyEnv:   apiKeyEnv,
		SupervisorBaseURL:     supervisorBaseURL(backend, raw.Supervisor.BaseURL),
		SupervisorAuthPath:    raw.Supervisor.AuthPath,

		WorkerBackend:        workerBackend,
		WorkerBuilderAgent:   defaultString(raw.Worker.BuilderAgent, "builder"),
		WorkerEvaluatorAgent: defaultString(raw.Worker.EvaluatorAgent, "evaluator"),
		WorkerToolsmithAgent: defaultString(raw.Worker.ToolsmithAgent, "toolsmith"),
		WorkerTimeoutSeconds: defaultInt(raw.Worker.TimeoutSeconds, 1800),
		WorkerShowReasoning:  showReasoning,
		WorkerAgentsDir:      raw.Worker.AgentsDir,
		ToolsRestartExitCode: defaultInt(raw.Tools.RestartExitCode, 75),
		ToolsDir:             toolsDir,

		CursorBinary:     defaultString(raw.Cursor.Binary, "cursor"),
		CursorModel:      cursorModel,
		ClaudeCodeBinary: defaultString(raw.ClaudeCode.Binary, "claude"),
		ClaudeCodeModel:  claudeModel,

		MaxIterations:    maxIter,
		PauseSeconds:     defaultInt(raw.Loop.PauseSeconds, 2),
		Interactive:      interactive,
		AdditionalPrompt: additionalPrompt,

		CheckpointPath:  checkpointPath,
		UserContextPath: userContextPath,
		OutputDir:       outputDirPath,
		ProjectRoot:     root,
		GoloopDir:       goloopDir,
		ConfigSources:   sources,
	}

	if cfg.Goal == "" {
		return nil, fmt.Errorf("no objective for this directory — run `goloop configure .` or pass --goal")
	}

	return cfg, nil
}

func (c *Config) WorkerModel() string {
	switch c.WorkerBackend {
	case WorkerClaudeCode:
		return c.ClaudeCodeModel
	default:
		return c.CursorModel
	}
}

func supervisorBaseURL(backend SupervisorBackend, configured string) string {
	if configured != "" {
		return configured
	}
	switch backend {
	case SupervisorAnthropic:
		return "https://api.anthropic.com/v1"
	default:
		return "https://api.openai.com/v1"
	}
}

func (c *Config) SupervisorLabel() string {
	label := string(c.SupervisorBackend)
	if c.SupervisorBackend == SupervisorChatGPT {
		label = "chatgpt"
	}
	return label + "/" + c.SupervisorModel
}

func (c *Config) ResolvedAgentsDir() string {
	if c.WorkerAgentsDir != "" {
		return filepath.Join(c.ProjectRoot, c.WorkerAgentsDir)
	}
	switch c.WorkerBackend {
	case WorkerClaudeCode:
		return filepath.Join(c.ProjectRoot, ".claude", "agents")
	default:
		return filepath.Join(c.ProjectRoot, ".cursor", "agents")
	}
}

func (c *Config) ToolsDirPath() string {
	if filepath.IsAbs(c.ToolsDir) {
		return c.ToolsDir
	}
	return filepath.Join(c.GoloopDir, filepath.Base(c.ToolsDir))
}

func MergePrompts(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func defaultInt(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

func defaultFloat(v, fallback float64) float64 {
	if v == 0 {
		return fallback
	}
	return v
}
