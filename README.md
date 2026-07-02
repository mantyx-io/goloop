<div align="center">

<img src="docs/logo.svg" alt="Goloop" width="92" />

# Goloop

**The agentic loop, in your terminal.**

A **supervisor** LLM plans every iteration. A **worker** — Cursor or Claude Code — ships the code.
Progress lives in plain markdown checkpoints. Point it at a goal and let it run.

[![Release](https://img.shields.io/github/v/release/mantyx-io/goloop?color=818cf8&label=release)](https://github.com/mantyx-io/goloop/releases)
[![CI](https://github.com/mantyx-io/goloop/actions/workflows/ci.yml/badge.svg)](https://github.com/mantyx-io/goloop/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/mantyx-io/goloop?color=38bdf8&logo=go&logoColor=white)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-34d399.svg)](LICENSE)

[**Website**](https://mantyx-io.github.io/goloop/) · [**Install**](#-install) · [**Quick start**](#-quick-start) · [**How it works**](#-how-it-works) · [**Config**](#-configuration)

</div>

---

```text
   🧠  Supervisor              🛠️  Worker                 📝  Checkpoint
   plans the next move   →     edits the repo      →     state persisted to
   (JSON action)               (Cursor / Claude)         .goloop/checkpoint.md
        ▲                                                       │
        └───────────────────────────  loop  ───────────────────┘
```

Goloop runs a tight, observable agent loop. The supervisor never edits files; it reasons about the
objective and emits a structured action each iteration. A worker carries out the actual code changes.
All state — progress, blockers, the iteration log, human answers — is written to human-readable
markdown you can diff, edit, and trust.

## ✨ Highlights

- **🔁 Supervisor / worker split** — the planner never touches files; a dedicated worker does the edits.
- **🔌 Pluggable backends** — supervise with **ChatGPT**, **OpenAI**, or **Anthropic**; execute with **Cursor CLI** or **Claude Code**.
- **📒 Markdown state** — no opaque DB; `.goloop/checkpoint.md` is the single source of truth.
- **🧰 Self-extending tools** — the loop can write its own supervisor tools, exit with code `75`, and auto-restart to pick them up.
- **🙋 Human-in-the-loop** — when only you can answer, it pauses with `ask_user` instead of guessing.
- **🪄 Rich TUI** — streamed worker reasoning, a live iteration log, and full-screen wizards for `configure` / `init`.
- **🧱 Layered config** — set models & auth once globally; set the goal per project; override per run.
- **📦 One static binary** — no runtime, no dependencies. `curl | bash` and you're running.

## 📦 Install

```bash
curl -fsSL https://raw.githubusercontent.com/mantyx-io/goloop/main/scripts/install.sh | bash
```

<details>
<summary>Other install options</summary>

```bash
# No sudo — install to ~/.local/bin
INSTALL_DIR="$HOME/.local/bin" \
  curl -fsSL https://raw.githubusercontent.com/mantyx-io/goloop/main/scripts/install.sh | bash

# Pin a specific release
curl -fsSL https://raw.githubusercontent.com/mantyx-io/goloop/main/scripts/install.sh | bash -s -- --version v1.0.1

# With Go
go install github.com/mantyx-io/goloop/cmd/goloop@latest
```

Pre-built binaries for **macOS**, **Linux** (`amd64`/`arm64`), and **Windows** (`amd64`) are on
[GitHub Releases](https://github.com/mantyx-io/goloop/releases).
</details>

## 🚀 Quick start

```bash
# 1. Global defaults — once. Pick provider, models, and sign in.
goloop configure
goloop login                              # ChatGPT subscription (device code)

# 2. Point it at a goal and run.
cd your-project
goloop run . --goal "Build a todo CLI"
```

That's it. If the directory isn't set up yet, `goloop run` launches a short setup wizard and then
continues into the loop automatically.

```bash
goloop run .                              # uses the project's saved goal
goloop run . --iters 30                   # cap iterations
goloop run . --dry-run                    # validate config without calling models
goloop run ./app -p "Focus on tests"      # extra instructions for this run

./scripts/run-loop.sh .                   # auto-restart when tools are installed (exit 75)
```

## 🧠 How it works

Each iteration the supervisor reads the checkpoint, decides what to do, and emits a JSON action:

```jsonc
{
  "action": "delegate",          // delegate | evaluate | ask_user | delegate_tools | complete
  "phase": "build",              // bootstrap | build | test | polish | integration
  "delegate_task": "Implement add/list/done commands",
  "checkpoint_update": {
    "completed":  ["CLI scaffold"],
    "next_steps": ["persist todos to disk"]
  },
  "status": "success"
}
```

| Action | What happens |
|--------|--------------|
| `delegate` | Sends a focused task to the worker (builder role) to edit the repo, then runs the `verify` command if configured |
| `evaluate` | Runs the worker read-only to review progress against the objective |
| `ask_user` | Pauses and asks the human a question (answers saved to `user_context.md`) |
| `delegate_tools` | Worker writes a new supervisor tool under `.goloop/tools/`; loop restarts (exit `75`) |
| `complete` | Claim is audited by a read-only evaluator pass; the loop stops only when it confirms |

Worker **role prompts** (builder / evaluator / toolsmith) are built into goloop, so a fresh project
needs no extra files. To customize a role, drop a `<role>.md` agent file (e.g. `builder.md`) into
`.cursor/agents/` or `.claude/agents/` and it will be used instead of the default.

## 🔐 Authentication

| Method | Command | Notes |
|--------|---------|-------|
| ChatGPT subscription | `goloop login` | Device-code flow (default, no port conflicts) |
| Browser callback | `goloop login --browser` | Uses `localhost:1455` (may conflict with Codex) |
| OpenAI API key | `goloop login --api-key` | Platform usage billing |
| Anthropic API key | `export ANTHROPIC_API_KEY=…` | Claude supervisor via API |
| Existing Codex CLI | _automatic_ | Reads `~/.codex/auth.json` |

Credentials are stored in `~/.goloop/auth.json` (Codex-compatible). Check status with
`goloop login status`.

## 🧱 Configuration

Config is layered — set models once globally, set the goal per project, override per run:

| Layer | Path | Purpose |
|-------|------|---------|
| **Global** | `~/.goloop/config.yaml` | Supervisor, worker, models, loop defaults |
| **Project** | `<dir>/.goloop/config.yaml` | `objective` / `goal` (required to run) |
| **Run flags** | `goloop run --goal …` | One-off overrides for a single run |

Runtime state always lives under `<dir>/.goloop/` (`checkpoint.md`, `user_context.md`, `tools/`).
See [`example/global-config.example.yaml`](example/global-config.example.yaml) and
[`example/.goloop/config.yaml`](example/.goloop/config.yaml).

<details>
<summary>Config sections</summary>

| Section | Purpose |
|---------|---------|
| `objective` / `goal` | Mission statement (project layer) |
| `supervisor` | Planning LLM — `chatgpt`, `openai`, or `anthropic` |
| `worker` | Execution backend — `cursor` or `claude_code` |
| `cursor` | Cursor CLI binary and model |
| `claude_code` | Claude Code CLI binary and model |
| `loop` | Iteration limit, pause, interactivity, `audit_completion`, `notifications`, `auto_commit`, `transcript`, `max_tokens` |
| `paths` | Output dir (checkpoint paths default under `.goloop/`) |
| `tools` | Tool directory and restart exit code |
| `verify` | Command run after every delegation and before accepting completion (e.g. `go test ./...`) |
</details>

## 🛠️ Commands

```text
goloop run [directory]        Run the agentic loop (default: .)
goloop configure [directory]  Global or project config (full-screen TUI wizard)
goloop init [directory]       Initialize a project (.goloop/config.yaml)
goloop login                  Authenticate the supervisor
goloop version                Print version
```

<details>
<summary><code>goloop run</code> flags</summary>

```text
--goal, -g          Objective for this run (overrides project config)
--iters             Max iterations (alias: --max-iterations)
-p, --prompt        Extra instructions for this run
--prompt-file       Read extra instructions from a file
--reset             Wipe .goloop state and output dir
--dry-run           Validate config without calling models
--plain             Disable rich UI
--no-interactive    Never prompt stdin (ask_user → blocker)
--supervisor-backend / --supervisor-model
--worker-backend / --cursor-model / --claude-code-model
```
</details>

<details>
<summary><code>goloop configure</code> &amp; <code>goloop init</code></summary>

```text
goloop configure            # global defaults → ~/.goloop/config.yaml
goloop configure .          # project objective → .goloop/config.yaml
goloop init                 # project setup wizard (goal, output dir, iters)
goloop init --yes --goal "Build a REST API" --iters 40
```

TUI keys: `enter` select · `tab` next · `shift+tab` / `esc` back · `/` filter models · `ctrl+c` quit
</details>

## 🤝 Contributing

Contributions are welcome! To hack on goloop:

```bash
git clone https://github.com/mantyx-io/goloop
cd goloop
go build ./...
go test ./...

# Build & install a local dev binary as `goloop-dev`
./scripts/local-install.sh
```

Please open an issue to discuss substantial changes before sending a PR, keep `go test ./...` green,
and run `gofmt`.

## 🙏 Acknowledgements

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and friends.

## 📄 License

[MIT](LICENSE) © The Goloop Authors
