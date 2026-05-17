# kode

The fastest, minimal, zero-dependency Go autonomous agent runtime.

`kode` runs the ReAct (Reasoning + Acting) loop — "think, therefore act" — as a single binary. No frameworks, no SDKs, no Python venvs. Just one loop and your tools.

```bash
kode run "How many lines in go.mod?"
# → 3 lines

kode run "Fix the OOM bug in default-hooks.js"
# → [reads file, edits code, runs tests, reports result]
```

## Design

| Principle | Implementation |
|-----------|---------------|
| **Zero deps** | `net/http`, `encoding/json`, `context`. That's it. |
| **LLM-agnostic** | Any OpenAI-compatible endpoint (Deepseek, OpenAI, etc.) |
| **Tool-first** | Tools are the only extension point. No chains, no prompts. |
| **Sandbox-ready** | `kode run --sandbox` → isolated Docker container, destroyed on exit |
| **Single binary** | `go build` → one file. Drop it anywhere. |

## Install

### go install (recommended)

```bash
go install github.com/BackendStack21/kode/cmd/kode@latest
```

Zero dependencies — the binary compiles in seconds.

### From source

```bash
git clone https://github.com/BackendStack21/kode.git
cd kode
go build -o kode ./cmd/kode
```

### Binary download

```bash
# Linux amd64
curl -fsSL https://github.com/BackendStack21/kode/releases/latest/download/kode-linux-amd64 -o kode
chmod +x kode
sudo mv kode /usr/local/bin/

# macOS arm64 (Apple Silicon)
curl -fsSL https://github.com/BackendStack21/kode/releases/latest/download/kode-darwin-arm64 -o kode
chmod +x kode
sudo mv kode /usr/local/bin/
```

## Quick Start

```bash
# Set your API key (Deepseek, OpenAI, or any compatible provider)
export DEEPSEEK_API_KEY=sk-...

# Run a task
kode run "List the files in this directory"

# Use a different model
kode run --model gpt-4o "Write a Go test for the loop engine"
```

## Security

With `--sandbox`, each session runs in a fresh Docker container:
- **No host filesystem access** beyond the working directory
- **No network** (unless `--allow-network`)
- **No capabilities** (`--cap-drop ALL`)
- **Destroyed on exit** (`docker run --rm`)

## Architecture

```
kode run "task"
  │
  ├─→ llm.Call()         # THINK: send messages to LLM
  │   └─→ tool_calls?    # Model requests action
  │       ├─→ tool.Call() # ACT: execute tool
  │       └─→ loop back   # Observe result, think again
  │
  └─→ final answer        # No more tool calls = done
```

## License

MIT
