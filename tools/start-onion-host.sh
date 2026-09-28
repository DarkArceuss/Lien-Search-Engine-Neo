#!/data/data/com.termux/files/usr/bin/bash
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
ONION_HOSTER_DIR="${ONION_HOSTER_DIR:-$HOME/Onion-Hoster}"
ONION_HOSTER="$ONION_HOSTER_DIR/termux.sh"
PORT="${LIEN_PORT:-3000}"
PID_FILE="$ROOT/.lien.pid"
RUNTIME_DIR="${LIEN_RUNTIME_DIR:-$HOME/.cache/lien}"
SERVER_BIN="$RUNTIME_DIR/lien-server"

if [ ! -f "$ONION_HOSTER" ]; then
    printf '%s\n' "Onion-Hoster was not found at: $ONION_HOSTER_DIR" >&2
    printf '%s\n' "Clone it first: git clone https://github.com/uzairdeveloper223/Onion-Hoster.git ~/Onion-Hoster" >&2
    exit 1
fi

cd "$ROOT"

if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' "Go is required. Install it with: pkg install golang" >&2
    exit 1
fi

if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    printf '%s\n' "Lien is already running on port $PORT."
else
    mkdir -p "$RUNTIME_DIR"
    go build -o "$SERVER_BIN" ./scripts
    chmod 700 "$SERVER_BIN"
    LIEN_BIND="127.0.0.1:$PORT" LIEN_CLEAR_VIA_TOR="1" LIEN_TOR_SOCKS="${LIEN_TOR_SOCKS:-127.0.0.1:9050}" nohup "$SERVER_BIN" >"$ROOT/.lien.log" 2>&1 &
    echo $! > "$PID_FILE"
    sleep 1
fi

if ! curl -fsS "http://127.0.0.1:$PORT/" >/dev/null; then
    printf '%s\n' "Lien failed to start. Check $ROOT/.lien.log" >&2
    exit 1
fi

cd "$ONION_HOSTER_DIR"
./termux.sh install tor
./termux.sh method custom_port "$PORT"
./termux.sh start
./termux.sh address
