# Proposal: Default Communication Channel for odek

**Goal:** Allow external systems (cron, webhooks, other tools) to invoke `odek run` and have the output/result reach the operator via a configurable communication channel. Telegram first, extensible later.

---

## 1. What Hermes Does

Hermes has a multi-channel architecture. Each platform (Telegram, Mattermost, Matrix, WhatsApp) has its own config section with allowed chats/rooms, channel prompts, etc. When cron jobs run, `wrap_response: true` in the `cron:` section wraps the output and routes it through the configured channels to reach the operator.

**Key Hermes patterns we're adopting:**
- Top-level `notify` configuration that's platform-agnostic
- Channel routing: "send this to the operator via channel X"
- Non-interactive awareness: different behavior when no TTY

---

## 2. Proposed Design

### 2.1 New `notify` config section

```json
// ~/.odek/config.json
{
  "notify": {
    "channel": "telegram",
    "telegram_chat_id": 8592463065
  }
}
```

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `channel` | string | Which channel to use: `"telegram"` (default: `""` = stdout only) |
| `telegram_chat_id` | int64 | Target chat ID for Telegram notifications |

**Future channels** (not implementing now, but design accommodates):
- `"webhook"` — POST to a URL
- `"email"` — SMTP
- `"discord"` — Discord webhook

### 2.2 Config wiring (7-layer chain)

Following odek's existing pattern (global → project → env → CLI):

| Layer | Key/Flag |
|-------|----------|
| `~/.odek/config.json` | `notify.channel`, `notify.telegram_chat_id` |
| `./odek.json` | same keys (project overrides) |
| Env var | `ODEK_NOTIFY_CHANNEL`, `ODEK_NOTIFY_TELEGRAM_CHAT_ID` |
| CLI flag | `--notify-channel`, `--notify-telegram-chat-id` |

### 2.3 Go types

**`internal/notify/notify.go`** (new package):

```go
package notify

// Config holds the notification routing configuration.
type Config struct {
    Channel        string `json:"channel,omitempty"`          // "telegram" or ""
    TelegramChatID int64  `json:"telegram_chat_id,omitempty"` // target chat
}

func DefaultConfig() Config {
    return Config{} // channel="" means stdout only
}
```

**Added to `FileConfig`** in `internal/config/loader.go`:

```go
Notify *notify.Config `json:"notify,omitempty"`
```

**Added to `ResolvedConfig`** in `internal/config/loader.go`:

```go
Notify notify.Config
```

### 2.4 Integration point: `odek run`

In `cmd/odek/main.go` → `run()`, after the agent completes:

```go
// After agent.Run() returns:
if !isTerminal() && resolved.Notify.Channel == "telegram" && resolved.Notify.TelegramChatID != 0 {
    // Send the final answer to the operator via Telegram
    sendTelegramNotification(resolved, result)
}
```

The `sendTelegramNotification` function:
1. Creates a minimal Bot client using the existing `Telegram.Token` from config
2. Sends the agent's final answer as a Markdown message to the configured `TelegramChatID`
3. Also sends any errors if the run failed

### 2.5 When does notification fire?

| Scenario | Notify? |
|----------|---------|
| `odek run` from terminal (TTY) | No — user sees output directly |
| `odek run` from cron (no TTY) | Yes — if `notify.channel` is configured |
| `odek run` from another script (no TTY) | Yes — same as cron |
| `odek telegram` (bot mode) | No — already interactive via Telegram |
| `odek repl` | No — interactive |

**Detection:** Use `!isTerminal()` (we already check stdin for color disable — reuse `terminal.IsTerminal`).

---

## 3. Files Changed

| File | Change |
|------|--------|
| `internal/notify/notify.go` | **NEW** — Config type + DefaultConfig |
| `internal/notify/notify_test.go` | **NEW** — Tests for config validation |
| `internal/config/loader.go` | Add `Notify` to `FileConfig`, `CLIFlags`, `ResolvedConfig`; wire env vars + CLI flags |
| `cmd/odek/main.go` | Add `--notify-channel`, `--notify-telegram-chat-id` flags; add post-run notification dispatch in `run()` |

---

## 4. Cron Usage Example

Once configured, a cron job like this:

```bash
# /etc/cron.d/odek-health
0 9 * * * root /usr/local/bin/odek run "Check system health and report" >> /var/log/odek-cron.log 2>&1
```

Would:
1. Run the agent task
2. Log raw output to `/var/log/odek-cron.log`
3. **Also** send the final answer via Telegram to chat ID `8592463065`

The operator gets the report on their phone without checking logs.

---

## 5. What We Don't Do (Yet)

- **Streaming/progress updates** — notification sends only the final answer, not intermediate tool calls
- **Multi-channel fanout** — only one channel at a time
- **Per-task channel override** — uses global config only
- **Channel-specific formatting** — Telegram uses Markdown, future channels will adapt

---

## 6. Implementation Phases

### Phase 1: Types + Config (2 files, ~80 LOC)
- Create `internal/notify/notify.go` 
- Wire into `loader.go` (FileConfig, ResolvedConfig, env vars, CLI flags, default overlay)

### Phase 2: Dispatch logic (1 file, ~50 LOC)  
- Add post-run notification to `cmd/odek/main.go` → `run()`
- `sendTelegramNotification()` helper

### Phase 3: Tests
- Config loading tests (defaults, env vars, CLI override)
- Notification dispatch test (mock Telegram server)

---

## 7. Verification

```bash
# Config loading
grep -o '"notify"' ~/.odek/config.json

# Manual test (with TTY — no notification fires)
odek run --notify-channel telegram --notify-telegram-chat-id 8592463065 "Say hello"

# Cron simulation (no TTY — notification SHOULD fire)
echo "Say hello" | odek run --notify-channel telegram --notify-telegram-chat-id 8592463065

# Full test suite
go test ./internal/notify/... ./internal/config/... -count=1
```
