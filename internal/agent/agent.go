package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var frontmatterRE = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n(.*)$`)

type Definition struct {
	Name         string
	SystemPrompt string
	ApprovalMode string
}

// Role identifies the worker role the supervisor is delegating to.
type Role string

const (
	RoleBuilder   Role = "builder"
	RoleEvaluator Role = "evaluator"
	RoleToolsmith Role = "toolsmith"
)

// builtinPrompts hold the default system prompts for each worker role. They
// replace the per-project .cursor/agents and .claude/agents scaffolding so a
// fresh project works without any extra files. A user can still override a role
// by placing a matching <name>.md agent file in the worker's agents dir.
var builtinPrompts = map[Role]string{
	RoleBuilder: `You are the Builder — an autonomous coding agent working inside an existing repository.

Your job: implement the delegated task so it actually works.
- Make focused, complete changes that satisfy the task; don't leave TODOs or stubs.
- Match the existing code's language, structure, conventions, and style.
- Add or update tests when it makes sense, and make sure the build and tests pass.
- Prefer editing existing files over creating new ones; keep the change minimal and coherent.
- Do NOT touch goloop's own state: never edit .goloop/checkpoint.md or .goloop/user_context.md — the supervisor owns those.
- When finished, give a short summary: what you changed, which files, and how to verify it.`,

	RoleEvaluator: `You are the Evaluator — a read-only reviewer.

Your job: assess the current state of the repository against the objective and the supervisor's criteria.
- Do NOT modify any files. Only read, search, and run read-only checks.
- Determine what is done, what is missing, what is broken, and what is risky.
- Be concrete and specific: reference files, symbols, and line-level details.
- End with a prioritized, actionable list of next steps for the Builder.`,

	RoleToolsmith: `You are the Toolsmith — you extend the supervisor's capabilities.

Your job: implement new supervisor tools as YAML files under .goloop/tools/.
- Each tool file must include at least a name and a description.
- Keep tools small, composable, and safe to run.
- Only create or modify files under .goloop/tools/ unless the task explicitly says otherwise.
- When finished, list the tools you added and what each one does.`,
}

// Builtin returns the built-in Definition for a worker role.
func Builtin(role Role) *Definition {
	mode := ""
	if role == RoleEvaluator {
		mode = "plan"
	}
	return &Definition{
		Name:         string(role),
		SystemPrompt: strings.TrimSpace(builtinPrompts[role]),
		ApprovalMode: mode,
	}
}

func Load(agentsDir, name string) (*Definition, error) {
	path := filepath.Join(agentsDir, name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", path)
	}

	frontmatter, body := splitFrontmatter(string(data))
	meta := parseSimpleYAML(frontmatter)

	def := &Definition{
		Name:         meta["name"],
		SystemPrompt: strings.TrimSpace(body),
		ApprovalMode: firstNonEmpty(meta["approvalMode"], meta["approval_mode"]),
	}
	if def.Name == "" {
		def.Name = name
	}
	return def, nil
}

func WithContext(def *Definition, task string) string {
	return def.SystemPrompt + "\n\n---\n\n" + task
}

func splitFrontmatter(text string) (string, string) {
	match := frontmatterRE.FindStringSubmatch(text)
	if match == nil {
		return "", strings.TrimSpace(text)
	}
	return match[1], match[2]
}

func parseSimpleYAML(text string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") || !strings.Contains(stripped, ":") {
			continue
		}
		parts := strings.SplitN(stripped, ":", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		result[key] = value
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
