# Programmatic API

odek is designed to be used both as a CLI tool and as a **Go library**. Import `github.com/BackendStack21/kode` and build your own agents, tools, and workflows — all from the same binary.

```go
import "github.com/BackendStack21/kode"
```

---

## `odek.Tool` Interface

The only extension point. Tools are plain Go structs with four methods:

```go
type Tool interface {
    Name() string
    Description() string
    Schema() any           // JSON Schema object describing parameters
    Call(args string) (string, error)
}
```

- **Name** — unique identifier used by the LLM to invoke the tool. Lowercase, underscore-separated (e.g. `read_file`, `delegate_tasks`).
- **Description** — natural-language description of what the tool does. The LLM reads this to decide when to call it.
- **Schema** — a JSON Schema object (`map[string]any`) defining the tool's parameters. The LLM uses this to construct valid arguments.
- **Call** — the implementation. Receives JSON-marshalled arguments, returns a string result (rendered to the LLM in the next iteration).

### Example: Custom Tool

```go
type fileStatsTool struct{}

func (t *fileStatsTool) Name() string { return "file_stats" }

func (t *fileStatsTool) Description() string {
    return "Get file statistics: line count, word count, character count."
}

func (t *fileStatsTool) Schema() any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "path": map[string]any{
                "type":        "string",
                "description": "Path to the file",
            },
        },
        "required": []string{"path"},
    }
}

func (t *fileStatsTool) Call(args string) (string, error) {
    var params struct {
        Path string `json:"path"`
    }
    if err := json.Unmarshal([]byte(args), &params); err != nil {
        return "", err
    }
    data, err := os.ReadFile(params.Path)
    if err != nil {
        return fmt.Sprintf("error: %v", err), nil
    }
    lines := strings.Count(string(data), "\n")
    words := len(strings.Fields(string(data)))
    return fmt.Sprintf("lines: %d, words: %d, chars: %d", lines, words, len(data)), nil
}
```

---

## `odek.Config` Struct

All configuration for an `odek.Agent`. Fields with zero values fall back to sensible defaults.

```go
type Config struct {
    // Model identifier (e.g. "deepseek-v4-flash", "gpt-4o").
    // Default: "deepseek-chat"
    Model string

    // OpenAI-compatible API endpoint.
    // Default: "https://api.deepseek.com/v1"
    BaseURL string

    // API key for the LLM provider.
    // Falls back to DEEPSEEK_API_KEY, then OPENAI_API_KEY.
    APIKey string

    // Thinking depth (provider-specific):
    //   Deepseek: "enabled" | "disabled"
    //   OpenAI o-series: "low" | "medium" | "high"
    // Empty = model profile default (or provider default if profile has none).
    Thinking string

    // Tools available to the agent.
    Tools []Tool

    // Maximum think→act cycles (default: 90).
    MaxIterations int

    // System prompt injected at the start of every run.
    // If AGENTS.md exists in the working directory, its content is
    // appended automatically with a "Project Instructions" header.
    // Set NoProjectFile to true to skip this.
    SystemMessage string

    // Disable automatic AGENTS.md loading.
    NoProjectFile bool

    // Cleanup function called by Close() (e.g., destroy sandbox container).
    // Set by the CLI when --sandbox is active. When nil, Close() is a no-op.
    SandboxCleanup func() error

    // Terminal renderer for colored output. When nil, agent runs silently.
    Renderer *render.Renderer

    // Skills config. When nil, skills are disabled.
    Skills *skills.SkillsConfig

    // Pre-loaded skill manager. When nil, New() auto-loads from
    // ~/.odek/skills/ and ./.odek/skills/.
    SkillManager *skills.SkillManager

    // Directory for persistent memory storage.
    // Default: ~/.odek/memory/
    MemoryDir string

    // Memory system configuration (facts, buffer, episodes).
    // Default: memory.DefaultMemoryConfig()
    MemoryConfig memory.MemoryConfig
}
```

---

## Agent Constructor

```go
func New(cfg Config) (*Agent, error)
```

Creates a new agent with the given configuration. The constructor:

1. Applies defaults for missing fields (`Model`, `BaseURL`, `MaxIterations`)
2. Resolves the API key (from config → `DEEPSEEK_API_KEY` → `OPENAI_API_KEY`)
3. Applies model profile defaults (thinking depth, timeout, context window)
4. Builds the internal tool registry from `cfg.Tools`
5. Loads `AGENTS.md` from the working directory (unless `NoProjectFile` is set)
6. Loads skills and injects auto-load skills into the system message
7. Creates the memory manager and injects fact/buffer context into the system prompt
8. Wires up lazy skill loading via the trigger-based trie index

Returns an error if no API key is found.

---

## `odek.Agent` Methods

### Run — Single-shot Task

```go
func (a *Agent) Run(ctx context.Context, task string) (string, error)
```

Executes the agent loop for a single task and returns the final answer. Best for one-off questions where conversation history isn't needed.

```go
result, err := agent.Run(ctx, "Refactor this module")
```

### RunWithMessages — Multi-Turn / Sessions

```go
func (a *Agent) RunWithMessages(ctx context.Context, messages []llm.Message) (string, []llm.Message, error)
```

Executes the agent loop starting from a pre-built message history. Returns the final answer plus the complete updated message history. Use this for multi-turn conversations where you load a prior session, append the new user message, and persist the updated history afterwards.

```go
messages := sess.GetMessages()
messages = append(messages, llm.Message{Role: "user", Content: input})
answer, allMessages, err := agent.RunWithMessages(ctx, messages)
store.Append(sess.ID, allMessages[origLen:])
```

### Token Tracking

```go
func (a *Agent) TotalInputTokens() int
func (a *Agent) TotalOutputTokens() int
```

Returns cumulative token counts from the most recent `Run` / `RunWithMessages` call. Use after each turn for session-level token economics.

### Memory Access

```go
func (a *Agent) Memory() *memory.MemoryManager
```

Returns the agent's memory manager for direct manipulation (buffer appends, episode extraction). Returns `nil` if memory is disabled. Used by the CLI layer after each turn and on session end.

### Close

```go
func (a *Agent) Close() error
```

Cleans up resources. If a `SandboxCleanup` function was set, it's called here (e.g., destroys the Docker sandbox container). **Always call `Close()` when done.**

```go
defer agent.Close()
```

---

## Model Profiles

Profiles provide per-model defaults for thinking depth, timeout, and context window limits. They are matched by **longest model-name prefix** — a profile for `deepseek-v4-flash` matches before a broader `deepseek-` profile.

### Built-in Profiles

| Prefix | Label | Default Thinking | Timeout | Max Context |
|--------|-------|-----------------|---------|-------------|
| `deepseek-v4-pro` | DeepSeek v4 Pro | enabled | 180s | 1,000,000 |
| `deepseek-v4-flash` | DeepSeek v4 Flash | — | 90s | 131,072 |
| `deepseek-` | DeepSeek (generic) | — | 120s | 131,072 |

### Adding a Profile

Append an entry to `KnownProfiles` — no changes to the LLM client, loop engine, or CLI needed:

```go
var KnownProfiles = []struct {
    Prefix  string
    Profile ModelProfile
}{
    {
        Prefix: "gpt-4o",
        Profile: ModelProfile{
            Label:      "GPT-4o",
            Timeout:    120,
            MaxContext: 128_000,
        },
    },
    // ...
}
```

### Lookup Functions

```go
func LookupProfile(model string) *ModelProfile
func ProfileLabel(model string) string
```

`LookupProfile` returns the best-matching profile (longest prefix). `ProfileLabel` returns the human-readable label or the model name if no profile matches.

---

## Project File (AGENTS.md)

```go
const ProjectFileName = "AGENTS.md"
func LoadProjectFile() string
```

`LoadProjectFile` reads `AGENTS.md` from the current working directory. When `NoProjectFile` is false (default), `New()` calls this automatically and appends the content to the system message with a `# Project Instructions` header. Use this for project-level conventions, architecture notes, and coding standards.

---

## Complete Example

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "strings"

    "github.com/BackendStack21/kode"
)

// Custom tool: count words in a string
type wordCountTool struct{}

func (t *wordCountTool) Name() string { return "word_count" }

func (t *wordCountTool) Description() string {
    return "Count the number of words in a given text."
}

func (t *wordCountTool) Schema() any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "text": map[string]any{
                "type":        "string",
                "description": "The text to count words in",
            },
        },
        "required": []string{"text"},
    }
}

func (t *wordCountTool) Call(args string) (string, error) {
    var params struct {
        Text string `json:"text"`
    }
    if err := json.Unmarshal([]byte(args), &params); err != nil {
        return "", err
    }
    count := len(strings.Fields(params.Text))
    return fmt.Sprintf("Word count: %d", count), nil
}

func main() {
    agent, err := odek.New(odek.Config{
        Model:         "deepseek-v4-flash",
        APIKey:        os.Getenv("DEEPSEEK_API_KEY"),
        SystemMessage: "You are an expert at analyzing text.",
        Tools:         []odek.Tool{&wordCountTool{}},
        MaxIterations: 10,
    })
    if err != nil {
        fmt.Fprintf(os.Stderr, "odek: %v\n", err)
        os.Exit(1)
    }
    defer agent.Close()

    result, err := agent.Run(context.Background(), "Count the words in the first paragraph of the README.")
    if err != nil {
        fmt.Fprintf(os.Stderr, "run: %v\n", err)
        os.Exit(1)
    }
    fmt.Println(result)
}
```

---

## Import Path

```
module github.com/your-module

go 1.25.0

require github.com/BackendStack21/kode v0.15.0
```

odek exposes a single package at `github.com/BackendStack21/kode`. All internal packages (`internal/llm`, `internal/memory`, `internal/skills`, etc.) are private and not importable outside the module. The public surface is:

| Symbol | Kind | Description |
|--------|------|-------------|
| `New` | func | Agent constructor |
| `Config` | struct | Agent configuration |
| `Agent` | struct | Agent runtime with Run, Close, Memory |
| `Tool` | interface | Tool plugin interface |
| `ModelProfile` | struct | Per-model defaults |
| `KnownProfiles` | var | Built-in model profiles |
| `LookupProfile` | func | Best-match profile lookup |
| `ProfileLabel` | func | Human-readable model label |
| `ProjectFileName` | const | AGENTS.md |
| `LoadProjectFile` | func | Read project instructions file |
