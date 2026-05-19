# MCP

kode has **two-way** Model Context Protocol support:

- **kode as MCP server** (`kode mcp`) — other agents (Claude Code, Cursor) use kode's tools
- **kode as MCP client** (config) — kode connects to external MCP servers and uses their tools

---

## kode as MCP Server (`kode mcp`)

Start kode as an MCP server over stdio. This lets Claude Code, Cursor, and other
MCP-compatible clients use kode's built-in tools.

```bash
kode mcp
```

### Claude Code setup

Add to `~/.claude/claude_dotfiles/claude.json` or your project's `.claude/settings.json`:

```json
{
  "mcpServers": {
    "kode": {
      "command": "kode",
      "args": ["mcp"]
    }
  }
}
```

For **Cursor**, add the same entry in Cursor Settings → MCP Servers.

### Exposed tools

| Tool | Description |
|------|-------------|
| `shell` | Run shell commands (with security classification) |
| `read_file` | Read files with line numbers and pagination |
| `write_file` | Write content to files (creates directories) |
| `search_files` | Search file contents or find files by name |
| `patch` | Find-and-replace edits with fuzzy matching |
| `browser` | Navigate web pages, take snapshots, click elements |

The `delegate_tasks` and `memory` tools are **not** exposed via MCP — they are
specific to kode's own agent loop.

### Sandbox

```bash
kode mcp --sandbox
```

All shell commands run inside a Docker container with `--cap-drop ALL`,
`--security-opt no-new-privileges`, resource limits, and noexec tmpfs.

### Security

Same `DangerousConfig` as `kode run`. No TTY in MCP mode → `non_interactive`
fallback applies (default: allow). Configure per-class overrides in `kode.json`:

```json
{
  "dangerous": {
    "non_interactive": "deny",
    "classes": {
      "network_egress": "deny",
      "code_execution": "prompt"
    }
  }
}
```

### Protocol

Stdio transport with JSON-RPC 2.0:

- `initialize` — protocol handshake (`protocolVersion: "2025-03-26"`)
- `tools/list` — returns all available tools with schemas
- `tools/call` — invokes a tool with the given arguments
- `ping` — health check (returns empty object)
- `initialized` — notification

Logging goes to stderr; stdin/stdout are reserved for the MCP protocol.

---

## kode as MCP Client

kode can connect to **external MCP servers** and expose their tools to the agent
during `kode run`, `kode repl`, `kode serve`, and `kode mcp`.

### Configuration

Add `mcp_servers` to `kode.json` (project-level) or `~/kode/config.json` (global):

```json
{
  "mcp_servers": {
    "playwright": {
      "command": "npx",
      "args": ["@playwright/mcp"]
    },
    "fetch": {
      "command": "uvx",
      "args": ["mcp-server-fetch"]
    },
    "github": {
      "command": "node",
      "args": ["/path/to/github-mcp-server/index.js"],
      "env": {
        "GITHUB_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  }
}
```

Each server is defined by:
- `command` — the executable to run
- `args` — optional command-line arguments
- `env` — optional environment variable overrides (empty string removes the variable)

The format matches Claude Code's `mcpServers` config — any MCP server you use
with Claude Code can be added to kode's config.

### How it works

On startup, kode:
1. Spawns each configured MCP server as a subprocess
2. Performs the MCP handshake (`initialize`)
3. Discovers all tools via `tools/list`
4. Registers each tool as `<server_name>__<tool_name>` (e.g., `playwright__navigate`)
5. Forwards `tools/call` requests to the appropriate server
6. Cleans up all server processes on exit

### Naming

Tools are prefixed with the server name to avoid collisions between servers:

- `playwright__navigate` — from the `playwright` server
- `fetch__fetch` — from the `fetch` server
- `github__search_issues` — from the `github` server

### What MCP servers work

Any server that implements the MCP stdio transport with `tools/list` and
`tools/call`. Common examples:

- **Playwright MCP** (`npx @playwright/mcp`) — browser automation
- **Fetch MCP** (`uvx mcp-server-fetch`) — HTTP requests
- **GitHub MCP** — repository management
- **SQLite MCP** — database queries
- **Filesystem MCP** — file operations
- **Docker MCP** — container management

### Lifecycle

MCP server processes are spawned when kode starts and killed when kode exits
(via `defer`). Each process gets its own stdin/stdout pipes — stderr from
MCP servers is shown in the kode console.

### Logging

kode logs MCP server connections to stderr:

```
kode: connected MCP server "playwright" (5 tools)
kode: connected MCP server "fetch" (1 tool)
```

Errors during discovery are reported at startup — the server is skipped and
kode continues with the remaining servers.

### Config reference

```json
{
  "mcp_servers": {
    "my-server": {
      "command": "command",
      "args": ["arg1", "arg2"],
      "env": {
        "API_KEY": "${MY_API_KEY}",
        "REMOVE_ME": ""
      }
    }
  }
}
```
