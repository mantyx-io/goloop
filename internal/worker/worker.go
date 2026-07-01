package worker

import (
	"context"
	"fmt"

	"github.com/mantyx-io/goloop/internal/agent"
	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/display"
)

type Result struct {
	ReturnCode int
	Stdout     string
	Stderr     string
	Command    []string
	Reasoning  string
}

func (r Result) OK() bool {
	return r.ReturnCode == 0
}

type Runner interface {
	RunBuilder(ctx context.Context, task string) (Result, error)
	RunEvaluator(ctx context.Context, task string) (Result, error)
	RunToolsmith(ctx context.Context, task string) (Result, error)
}

func New(cfg *config.Config, disp *display.Display) (Runner, error) {
	switch cfg.WorkerBackend {
	case config.WorkerCursor:
		return NewCursor(cfg, disp), nil
	case config.WorkerClaudeCode:
		return NewClaudeCode(cfg, disp), nil
	default:
		return nil, fmt.Errorf("unsupported worker backend: %s (use cursor or claude_code)", cfg.WorkerBackend)
	}
}

// resolveAgent returns the prompt definition for a worker role. It prefers a
// user-provided agent file (when one exists in the worker's agents dir) and
// otherwise falls back to the built-in role prompt, so no scaffolding is needed.
func resolveAgent(cfg *config.Config, role agent.Role) *agent.Definition {
	if name := roleAgentName(cfg, role); name != "" {
		if def, err := agent.Load(cfg.ResolvedAgentsDir(), name); err == nil {
			return def
		}
	}
	return agent.Builtin(role)
}

func roleAgentName(cfg *config.Config, role agent.Role) string {
	switch role {
	case agent.RoleBuilder:
		return cfg.WorkerBuilderAgent
	case agent.RoleEvaluator:
		return cfg.WorkerEvaluatorAgent
	case agent.RoleToolsmith:
		return cfg.WorkerToolsmithAgent
	}
	return ""
}
