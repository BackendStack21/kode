---
name: rebuild-odek-telegram
description: Rebuild the odek binary and restart the Telegram bot after a new release is created
version: 1.0.0
author: odek
auto_load: false
odek:
  trigger:
    topic: release tag deploy update build telegram bot restart
    action: rebuild restart deploy update
---

# Rebuild & Restart odek Telegram Bot

After a new release is tagged and published (e.g., via `git tag` and `gh release create`), the odek binary must be rebuilt and the running Telegram bot process restarted for the changes to take effect.

## Overview

This skill automates the post-release steps:
1. Pull the latest code from the repository
2. Build the odek binary
3. Restart the running Telegram bot process

## Steps

### 1. Pull latest code

```bash
cd /root/projects/odek
git pull origin main --tags
```

Verify the new tag is present:
```bash
git tag --sort=-v:refname | head -3
```

### 2. Rebuild the binary

```bash
go build -o odek ./cmd/odek
```

Verify the build succeeded and check the version:
```bash
./odek version
```

### 3. Restart the Telegram bot

The odek Telegram bot handles restart via SIGHUP signal (implemented in `cmd/odek/telegram.go`). Sending SIGHUP causes the bot to re-exec itself, picking up the new binary.

Find the running bot process:
```bash
pgrep -f "odek telegram" || ps aux | grep "odek telegram" | grep -v grep
```

Send SIGHUP to trigger a graceful restart:
```bash
pkill -SIGHUP -f "odek telegram" 2>/dev/null || kill -HUP $(pgrep -f "odek telegram")
```

If running under systemd:
```bash
systemctl restart odek-telegram
```

### 4. Verify the restart

Check that the bot process restarted successfully:
```bash
pgrep -f "odek telegram"
```

Monitor logs for successful startup:
```bash
# If using log file:
tail -f ~/.odek/telegram.log

# If logging to stderr/journald:
journalctl -u odek-telegram --since "1 minute ago"  # systemd
```

## Pitfalls

- **Build errors**: If `go build` fails, check for compilation errors and fix before restarting
- **Process not found**: The bot may be running under a different name or user (check with `ps aux | grep odek`)
- **SIGHUP not supported**: If the running bot is an older version that doesn't support SIGHUP restart, you may need to kill the process and start it manually:
  ```bash
  pkill -f "odek telegram"
  nohup ./odek telegram &
  ```
- **Permission denied**: If built as one user and running as another, ensure the binary is executable by the target user (`chmod +x odek`)

## Verification

After restarting, send a test message to the bot in Telegram to confirm it's operational:
- Send `/start` to verify the bot responds
- Send a simple task like `/help` to verify agent functionality
