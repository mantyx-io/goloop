# Goloop

Agentic loop CLI in Go. A **supervisor** LLM plans each iteration; a **worker** (Cursor by default) executes code changes. Progress is tracked in `.goloop/checkpoint.md` with a rich terminal UI.

Repository: [github.com/mantyx-io/goloop](https://github.com/mantyx-io/goloop)

Inspired by [frontier-experiment](https://github.com/vetro/frontier-experiment)'s Python `fiber-loop`.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/mantyx-io/goloop/main/scripts/install.sh | bash
```

Install to `~/.local/bin` (no sudo):

```bash
INSTALL_DIR="$HOME/.local/bin" curl -fsSL https://raw.githubusercontent.com/mantyx-io/goloop/main/scripts/install.sh | bash
```

Pin a release:

```bash
curl -fsSL https://raw.githubusercontent.com/mantyx-io/goloop/main/scripts/install.sh | bash -s -- --version v0.1.0
```

Pre-built binaries for `linux` and `darwin` (`amd64`, `arm64`) and `windows` (`amd64`) are published on [GitHub Releases](https://github.com/mantyx-io/goloop/releases).

## Quick start

```bash
# Or build from source
go install github.com/mantyx-io/goloop/cmd/goloop@latest

# 1. Global defaults (once — models, provider, auth)
goloop configure
goloop login                    # ChatGPT subscription (device code)

# 2. Per project — initialize
cd your-project
goloop init
goloop init --yes --goal "Build a todo CLI" --iters 30

# 3. Run the loop
goloop run .
goloop run . --iters=30
goloop run . --goal "Ship MVP tonight"   # one-off objective
goloop run . --dry-run

# With auto-restart when tools are installed (exit code 75)
./scripts/run-loop.sh .
```

Other auth options:

```bash
goloop login --browser          # Optional: localhost callback (conflicts with Codex)
export OPENAI_API_KEY=sk-... && goloop login --api-key
```

## Configuration

Config is layered — set models once globally, set the goal per project:

| Layer | Path | Purpose |
|-------|------|---------|
| Global | `~/.goloop/config.yaml` | Supervisor, worker, models, loop defaults |
| Project | `<dir>/.goloop/config.yaml` | `objective` / `goal` (required to run) |
| Legacy | `goloop.yaml` | Still supported for older projects |

Runtime state always lives under `<dir>/.goloop/` (`checkpoint.md`, `user_context.md`, `tools/`).

See `example/global-config.example.yaml` and `example/.goloop/config.yaml`.

| Section | Purpose |
|---------|---------|
| `objective` / `goal` | Mission statement (project layer) |
| `supervisor` | Planning LLM (`chatgpt`, `openai`, or `anthropic`) |
| `worker` | Execution backend (`cursor` or `claude_code`) |
| `cursor` | Cursor CLI binary and model |
| `claude_code` | Claude Code CLI binary and model |
| `loop` | Iteration limits, pause, interactivity |
| `paths` | Output dir (checkpoint paths default under `.goloop/`) |
| `tools` | Tool directory and restart exit code |

Override per run with `goloop run` flags (`--goal`, `--iters`, model overrides, etc.).

## Architecture

```
Supervisor (ChatGPT/OpenAI/Anthropic)  →  JSON plan each iteration
        ↓
Actions: delegate | evaluate | ask_user | delegate_tools | complete
        ↓
Worker (Cursor CLI)  →  edits files in the project root (configurable output dir)
        ↓
.goloop/checkpoint.md  ←  progress, blockers, iteration log
.goloop/user_context.md ←  human answers
```

Loop state lives under `.goloop/` in the target directory. Worker role prompts (builder/evaluator/toolsmith) are built into goloop; you can optionally override any role with a `<role>.md` agent file in `.cursor/agents/` or `.claude/agents/`.

## Authentication

| Method | Command | Billing |
|--------|---------|---------|
| ChatGPT subscription | `goloop login` | Device code (default CLI flow) |
| Browser callback | `goloop login --browser` | Uses localhost:1455 (may conflict with Codex) |
| OpenAI API key | `goloop login --api-key` | Platform usage billing |
| Anthropic API key | `export ANTHROPIC_API_KEY=...` | Claude supervisor via API |
| Existing Codex CLI | (automatic) | Reads `~/.codex/auth.json` |

Set `supervisor.backend: chatgpt` in your global config to use your ChatGPT subscription. Credentials are stored in `~/.goloop/auth.json` (Codex-compatible format).

Check status: `goloop login status`

## Commands

```
goloop run [directory]       Run the agentic loop (default directory: .)
goloop configure [directory] Global or project config
goloop init [directory]      Initialize project (.goloop/, agents)
goloop login                 Authenticate for the supervisor
goloop version               Print version
```

### `goloop init`

Project-level setup wizard (goal, output dir, iterations, interactive mode). Creates `.goloop/config.yaml`. Worker role prompts (builder/evaluator/toolsmith) are built in — no agent files are scaffolded.

```
goloop init
goloop init . --yes --goal "Build a REST API" --iters 40 --output-dir app

  --goal              Project objective
  --output-dir        Worker output directory (default: . — project root)
  --iters             Max iterations per run
  --interactive       Prompt stdin on ask_user (default: true)
  --no-interactive    Disable interactive mode
  --yes               Skip TUI
```

> Worker prompts are built in. To customize a role, drop a `<role>.md` agent file
> (e.g. `builder.md`) into `.cursor/agents/` or `.claude/agents/` and it will be used
> instead of the default.

### `goloop run`

```
goloop run . --iters=30
goloop run ./app -p "Focus on tests" --reset --dry-run

  --goal, -g          Objective for this run (overrides project config)
  --iters             Max iterations (alias: --max-iterations)
  -p, --prompt        Extra instructions for this run
  --prompt-file       Read extra instructions from file
  --reset             Wipe .goloop state and output dir
  --dry-run           Validate config without calling models
  --plain             Disable rich UI
  --no-interactive    Never prompt stdin (ask_user → blocker)
  --supervisor-backend / --supervisor-model
  --worker-backend / --cursor-model
```

### `goloop configure`

Launches a full-screen TUI wizard:

- **`goloop configure`** (global) — provider, auth, models, iterations → `~/.goloop/config.yaml`
- **`goloop configure .`** (project) — objective (+ optional overrides) → `.goloop/config.yaml`

Project wizard steps: objective → provider → auth → supervisor model → worker model → iterations → review.

```
goloop configure
goloop configure .
goloop configure . --yes --objective "Build a todo CLI" --iters 50

  --global            Write global defaults (default when no directory given)
  --yes               Skip TUI (flags / existing config only)
  --plain             Non-TTY fallback
  --objective         Project goal
  --supervisor-backend / --supervisor-model
  --worker-backend / --cursor-model
  --iters             Default max iterations
```

TUI keys: `ctrl+enter` continue · `/` focus model search · `enter` select · `esc` back · `ctrl+c` quit

## License

MIT
