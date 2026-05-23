#!/usr/bin/env bash
# build-and-restart-telegram.sh — Build odek and restart the Telegram bot.
#
# Usage:
#   ./build-and-restart-telegram.sh              # build + restart
#   ./build-and-restart-telegram.sh --build-only   # build only
#   ./build-and-restart-telegram.sh --restart-only # restart only
#
# The bot binary is installed to /usr/local/bin/odek.
# odek's own singleton lock (~/.odek/telegram.pid) prevents duplicates.

set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="/usr/local/bin/odek"
STDERR_LOG="/tmp/odek-telegram.log"

BUILD=true
RESTART=true

# ── Parse flags ──────────────────────────────────────────────────────────
for arg in "$@"; do
    case "$arg" in
        --build-only)   RESTART=false ;;
        --restart-only) BUILD=false   ;;
        *) echo "Unknown flag: $arg"; exit 1 ;;
    esac
done

# ── Build ────────────────────────────────────────────────────────────────
if $BUILD; then
    echo "🔨 Building odek..."
    cd "$PROJECT_DIR"
    go build -o "$BINARY" ./cmd/odek/
    echo "   ✓ Binary: $BINARY ($(du -h "$BINARY" | cut -f1))"
fi

# ── Restart ──────────────────────────────────────────────────────────────
if ! $RESTART; then
    echo "   (--build-only: skipping restart)"
    exit 0
fi

echo ""
echo "🔄 Restarting odek telegram bot..."

# Kill any running odek telegram process.
KILLED=false
while IFS= read -r pid; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
        echo "   Killing odek telegram (PID $pid)..."
        kill -HUP "$pid" 2>/dev/null || true
        KILLED=true
    fi
done < <(pgrep -f "odek telegram" 2>/dev/null || true)

# Wait for graceful shutdown (odek's singleton lock handles cleanup).
if $KILLED; then
    echo "   Waiting for graceful shutdown..."
    for i in $(seq 1 30); do
        if ! pgrep -f "odek telegram" >/dev/null 2>&1; then
            echo "   ✓ Old process exited after ${i}s"
            break
        fi
        sleep 1
    done

    # Force kill if still alive.
    if pgrep -f "odek telegram" >/dev/null 2>&1; then
        echo "   ⚠ Force-killing stubborn process..."
        pkill -9 -f "odek telegram" 2>/dev/null || true
        sleep 1
    fi
else
    echo "   No existing odek telegram process found."
fi

# Start the bot from the project directory so cwd is correct.
echo "   Starting odek telegram..."
cd "$PROJECT_DIR"

# Load secrets if available
if [ -f "$HOME/.odek/secrets.env" ]; then
    set -a; source "$HOME/.odek/secrets.env"; set +a
fi

exec "$BINARY" telegram 2>"$STDERR_LOG" &

new_pid=$!
echo "   ✓ odek telegram started (PID $new_pid)"
echo "   Stderr: $STDERR_LOG"

# Quick health check.
sleep 2
if kill -0 "$new_pid" 2>/dev/null; then
    echo "   ✓ Bot is running"
else
    echo "   ✗ Bot died immediately — check log:"
    tail -20 "$STDERR_LOG"
    exit 1
fi
