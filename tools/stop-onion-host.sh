#!/data/data/com.termux/files/usr/bin/bash
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
ONION_HOSTER_DIR="${ONION_HOSTER_DIR:-$HOME/Onion-Hoster}"
PID_FILE="$ROOT/.lien.pid"

if [ -f "$PID_FILE" ]; then
    PID="$(cat "$PID_FILE")"
    if kill -0 "$PID" 2>/dev/null; then
        kill "$PID" 2>/dev/null || true
    fi
    rm -f "$PID_FILE"
fi

if [ -x "$ONION_HOSTER_DIR/termux.sh" ]; then
    "$ONION_HOSTER_DIR/termux.sh" stop || true
fi
printf '%s\n' "Lien and Onion-Hoster service stopped."
