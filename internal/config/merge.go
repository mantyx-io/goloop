package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadMerged reads global + local (+ legacy) config layers into one rawConfig.
func LoadMerged(projectRoot, explicitPath string) (*rawConfig, []string, error) {
	root, err := os.ReadFile(GlobalConfigPath())
	var layers []string
	merged := &rawConfig{}

	if err == nil {
		var global rawConfig
		if err := yaml.Unmarshal(root, &global); err != nil {
			return nil, nil, fmt.Errorf("parse global config: %w", err)
		}
		mergeRaw(merged, &global)
		layers = append(layers, GlobalConfigPath())
	} else if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("read global config: %w", err)
	}

	localPath := LocalConfigPath(projectRoot)
	if explicitPath != "" {
		if filepath.IsAbs(explicitPath) {
			localPath = explicitPath
		} else {
			localPath = filepath.Join(projectRoot, explicitPath)
		}
	}

	if data, err := os.ReadFile(localPath); err == nil {
		var local rawConfig
		if err := yaml.Unmarshal(data, &local); err != nil {
			return nil, nil, fmt.Errorf("parse local config %s: %w", localPath, err)
		}
		mergeRaw(merged, &local)
		layers = append(layers, localPath)
	} else if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("read local config: %w", err)
	}

	if explicitPath == "" {
		if legacy := findLegacyConfig(projectRoot); legacy != "" {
			data, err := os.ReadFile(legacy)
			if err != nil {
				return nil, nil, err
			}
			var leg rawConfig
			if err := yaml.Unmarshal(data, &leg); err != nil {
				return nil, nil, fmt.Errorf("parse legacy config %s: %w", legacy, err)
			}
			mergeRaw(merged, &leg)
			layers = append(layers, legacy)
		}
	}

	if len(layers) == 0 {
		return nil, nil, fmt.Errorf("no config found — run `goloop configure` (global) and `goloop configure .` (project goal)")
	}

	applyDefaults(merged)
	return merged, layers, nil
}

func mergeRaw(dst, src *rawConfig) {
	if src == nil {
		return
	}
	if src.Goal != "" {
		dst.Goal = src.Goal
	}
	if src.Objective != "" {
		dst.Objective = src.Objective
	}
	mergeSupervisor(&dst.Supervisor, &src.Supervisor)
	mergeWorker(&dst.Worker, &src.Worker)
	mergeCursor(&dst.Cursor, &src.Cursor)
	mergeClaudeCode(&dst.ClaudeCode, &src.ClaudeCode)
	mergeTools(&dst.Tools, &src.Tools)
	mergeLoop(&dst.Loop, &src.Loop)
	mergePaths(&dst.Paths, &src.Paths)
	mergeVerify(&dst.Verify, &src.Verify)
}

func mergeVerify(dst, src *verifyYAML) {
	if src.Command != "" {
		dst.Command = src.Command
	}
	if src.TimeoutSeconds != 0 {
		dst.TimeoutSeconds = src.TimeoutSeconds
	}
}

func mergeSupervisor(dst, src *supervisorYAML) {
	if src.Backend != "" {
		dst.Backend = src.Backend
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.APIKeyEnv != "" {
		dst.APIKeyEnv = src.APIKeyEnv
	}
	if src.BaseURL != "" {
		dst.BaseURL = src.BaseURL
	}
	if src.Temperature != 0 {
		dst.Temperature = src.Temperature
	}
	if src.AuthPath != "" {
		dst.AuthPath = src.AuthPath
	}
}

func mergeWorker(dst, src *workerYAML) {
	if src.Backend != "" {
		dst.Backend = src.Backend
	}
	if src.BuilderAgent != "" {
		dst.BuilderAgent = src.BuilderAgent
	}
	if src.EvaluatorAgent != "" {
		dst.EvaluatorAgent = src.EvaluatorAgent
	}
	if src.ToolsmithAgent != "" {
		dst.ToolsmithAgent = src.ToolsmithAgent
	}
	if src.TimeoutSeconds != 0 {
		dst.TimeoutSeconds = src.TimeoutSeconds
	}
	if src.ShowReasoning != nil {
		dst.ShowReasoning = src.ShowReasoning
	}
	if src.AgentsDir != "" {
		dst.AgentsDir = src.AgentsDir
	}
}

func mergeCursor(dst, src *cursorYAML) {
	if src.Binary != "" {
		dst.Binary = src.Binary
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
}

func mergeClaudeCode(dst, src *claudeCodeYAML) {
	if src.Binary != "" {
		dst.Binary = src.Binary
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
}

func mergeTools(dst, src *toolsYAML) {
	if src.RestartExitCode != 0 {
		dst.RestartExitCode = src.RestartExitCode
	}
	if src.Dir != "" {
		dst.Dir = src.Dir
	}
}

func mergeLoop(dst, src *loopYAML) {
	if src.MaxIterations != 0 {
		dst.MaxIterations = src.MaxIterations
	}
	if src.PauseSeconds != 0 {
		dst.PauseSeconds = src.PauseSeconds
	}
	if src.Interactive != nil {
		dst.Interactive = src.Interactive
	}
	if src.AuditCompletion != nil {
		dst.AuditCompletion = src.AuditCompletion
	}
	if src.Notifications != nil {
		dst.Notifications = src.Notifications
	}
	if src.AutoCommit != nil {
		dst.AutoCommit = src.AutoCommit
	}
	if src.Transcript != nil {
		dst.Transcript = src.Transcript
	}
	if src.MaxTokens != 0 {
		dst.MaxTokens = src.MaxTokens
	}
	if src.AdditionalPrompt != "" {
		dst.AdditionalPrompt = src.AdditionalPrompt
	}
}

func mergePaths(dst, src *pathsYAML) {
	if src.Checkpoint != "" {
		dst.Checkpoint = src.Checkpoint
	}
	if src.UserContext != "" {
		dst.UserContext = src.UserContext
	}
	if src.OutputDir != "" {
		dst.OutputDir = src.OutputDir
	}
}
