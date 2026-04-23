#!/usr/bin/env zsh
set -euo pipefail

ROOT_DIR=${0:A:h}
STATE_DIR="$ROOT_DIR/.self-host"
BIN_DIR="$STATE_DIR/bin"
LOG_DIR="$STATE_DIR/logs"
BIN_PATH="$BIN_DIR/snowflakes"
ENV_PATH="$STATE_DIR/app.env"
STATE_PATH="$STATE_DIR/state.env"
LOG_PATH="$LOG_DIR/snowflakes.log"
DEV_LOG_PATH="$LOG_DIR/snowflakes-dev.log"
CADDYFILE="$HOME/Caddyfile"
SESSION_NAME="snowflakes-self-host"
DEV_SESSION_NAME="snowflakes-self-host-dev"
DEFAULT_PUBLIC_URL="http://justone.pinky.lilf.ir"
APP_HOST="127.0.0.1"
APP_PORT="3400"
CADDY_BEGIN="# BEGIN snowflakes self-host"
CADDY_END="# END snowflakes self-host"

PUBLIC_URL=""
SITE_ADDRESS=""
WORDPACK_DIR="${SNOWFLAKES_WORDPACK_DIR:-$HOME/.snowflakes/wordpacks}"
DEV_CHILD_PID=""

tmuxnew () {
	tmux kill-session -t "$1" &> /dev/null || true
	tmux new -d -s "$@"
}

stop_tmux_session() {
	tmux kill-session -t "$1" &>/dev/null || true
}

usage() {
	cat <<USAGE
Usage:
  ./self_host.zsh setup [public-url]
  ./self_host.zsh redeploy [public-url]
  ./self_host.zsh start
  ./self_host.zsh dev-start [public-url]
  ./self_host.zsh stop

Default public URL: $DEFAULT_PUBLIC_URL
Internal bind:      http://$APP_HOST:$APP_PORT
USAGE
}

die() {
	print -u2 -- "Error: $*"
	exit 1
}

note() {
	print -- "==> $*"
}

require_command() {
	command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"
}

normalize_public_url() {
	local input="${1:-$DEFAULT_PUBLIC_URL}"
	input="${input%/}"
	if [[ "$input" != *"://"* ]]; then
		input="http://$input"
	fi
	print -r -- "$input"
}

parse_public_url() {
	local url="$1"
	local scheme rest hostport raw_path

	[[ "$url" == http://* || "$url" == https://* ]] || die "Only http:// or https:// URLs are supported: $url"
	scheme="${url%%://*}"
	rest="${url#*://}"
	if [[ "$rest" == */* ]]; then
		hostport="${rest%%/*}"
		raw_path="/${rest#*/}"
	else
		hostport="$rest"
		raw_path=""
	fi
	[[ -n "$hostport" ]] || die "Could not parse host from $url"
	if [[ -n "$raw_path" && "$raw_path" != "/" ]]; then
		die "Path prefixes are not supported yet; use a host-only URL like http://justone.pinky.lilf.ir"
	fi
	PUBLIC_URL="$url"
	SITE_ADDRESS="$scheme://$hostport"
}

ensure_dirs() {
	mkdir -p "$BIN_DIR" "$LOG_DIR" "$WORDPACK_DIR"
}

write_env() {
	PORT="$APP_PORT" \
	NETWORK_ADDRESS="$APP_HOST" \
	ROOT_URL="$PUBLIC_URL" \
	SNOWFLAKES_PUBLIC_URL="$PUBLIC_URL" \
	SNOWFLAKES_WORDPACK_DIR="$WORDPACK_DIR" \
	python3 - "$ENV_PATH" <<'PY'
from pathlib import Path
import os
import shlex
import sys

env_path = Path(sys.argv[1])
base_names = [
    "PORT",
    "NETWORK_ADDRESS",
    "ROOT_URL",
    "SNOWFLAKES_PUBLIC_URL",
    "SNOWFLAKES_WORDPACK_DIR",
]
proxy_names = sorted(name for name in os.environ if name.lower().endswith("_proxy"))
lines = []
written = set()

for name in [*base_names, *proxy_names]:
    if name in written:
        continue
    value = os.environ.get(name)
    if value is None:
        continue
    lines.append(f"{name}={shlex.quote(value)}")
    written.add(name)

env_path.write_text("\n".join(lines) + "\n")
PY
}

write_state() {
	cat > "$STATE_PATH" <<EOF_STATE
PUBLIC_URL=$PUBLIC_URL
SITE_ADDRESS=$SITE_ADDRESS
WORDPACK_DIR=$WORDPACK_DIR
EOF_STATE
}

load_state() {
	[[ -f "$STATE_PATH" ]] || die "Missing $STATE_PATH. Run ./self_host.zsh setup [public-url] first."
	source "$STATE_PATH"
}

build_binary() {
	ensure_dirs

	local local_go_version
	local_go_version="$(GOWORK=off go env GOVERSION 2>/dev/null || true)"
	if [[ -n "$local_go_version" && "$local_go_version" != go1.25* ]]; then
		note "Local Go is $local_go_version; the first build may download Go 1.25 automatically."
	fi

	note "Generating templ components"
	(
		cd "$ROOT_DIR"
		templ generate -path internal/snowflakes
	)

	note "Building Snowflakes"
	(
		cd "$ROOT_DIR"
		GOWORK=off go build -buildvcs=false -o "$BIN_PATH" ./cmd/snowflakes
	)
}

render_caddy_block() {
	cat <<EOF_BLOCK
$SITE_ADDRESS {
    encode zstd gzip
    reverse_proxy $APP_HOST:$APP_PORT
}
EOF_BLOCK
}

write_caddy_block() {
	local block_file
	block_file="$(mktemp)"
	render_caddy_block > "$block_file"
	python3 - "$CADDYFILE" "$block_file" "$CADDY_BEGIN" "$CADDY_END" <<'PY'
from pathlib import Path
import sys

caddyfile = Path(sys.argv[1])
block = Path(sys.argv[2]).read_text().rstrip() + "\n"
begin = sys.argv[3]
end = sys.argv[4]

text = caddyfile.read_text() if caddyfile.exists() else ""
start = text.find(begin)
finish = text.find(end)
if start != -1 and finish != -1 and finish > start:
    finish += len(end)
    while finish < len(text) and text[finish] == "\n":
        finish += 1
    text = text[:start] + text[finish:]
text = text.rstrip() + "\n\n" + begin + "\n" + block + end + "\n"
caddyfile.write_text(text)
PY
	rm -f "$block_file"
}

reload_caddy() {
	note "Reloading Caddy"
	caddy reload --adapter caddyfile --config "$CADDYFILE"
}

stop_all_sessions() {
	note "Stopping tmux sessions $SESSION_NAME and $DEV_SESSION_NAME"
	stop_tmux_session "$SESSION_NAME"
	stop_tmux_session "$DEV_SESSION_NAME"
}

start_app() {
	ensure_dirs
	[[ -x "$BIN_PATH" ]] || die "Missing $BIN_PATH. Run setup or redeploy first."
	write_env
	stop_all_sessions
	note "Starting tmux session $SESSION_NAME"
	local cmd
	cmd="set -euo pipefail; source ${(q)ENV_PATH}; exec ${(q)BIN_PATH} >> ${(q)LOG_PATH} 2>&1"
	tmuxnew "$SESSION_NAME" zsh -lc "$cmd"
}

watch_signature() {
	python3 - "$ROOT_DIR" <<'PY'
from pathlib import Path
import hashlib
import sys

root = Path(sys.argv[1])
targets = []
for rel in ("go.mod", "go.sum", "self_host.zsh"):
    path = root / rel
    if path.exists():
        targets.append(path)

for base in (root / "cmd", root / "internal"):
    if not base.exists():
        continue
    for path in sorted(base.rglob("*")):
        if not path.is_file():
            continue
        if path.suffix not in {".go", ".templ", ".css", ".js", ".txt", ".svg"}:
            continue
        if path.name.endswith("_templ.go"):
            continue
        targets.append(path)

digest = hashlib.sha256()
for path in targets:
    stat = path.stat()
    digest.update(str(path.relative_to(root)).encode())
    digest.update(str(stat.st_mtime_ns).encode())
    digest.update(str(stat.st_size).encode())

print(digest.hexdigest())
PY
}

stop_dev_child() {
	if [[ -n "${DEV_CHILD_PID:-}" ]]; then
		kill "$DEV_CHILD_PID" &>/dev/null || true
		wait "$DEV_CHILD_PID" 2>/dev/null || true
		DEV_CHILD_PID=""
	fi
}

start_dev_child() {
	note "Launching development app"
	zsh -lc "source ${(q)ENV_PATH}; exec ${(q)BIN_PATH} >> ${(q)LOG_PATH} 2>&1" &
	DEV_CHILD_PID=$!
}

rebuild_and_restart_dev() {
	note "Detected source changes; rebuilding for development"
	if build_binary; then
		write_env
		stop_dev_child
		start_dev_child
		note "Development app running at $PUBLIC_URL"
	else
		note "Build failed; keeping the previous app process running"
	fi
}

run_dev_loop() {
	load_state
	parse_public_url "$PUBLIC_URL"
	ensure_dirs
	write_env

	trap 'stop_dev_child; exit 0' INT TERM EXIT

	rebuild_and_restart_dev

	local current_signature next_signature
	current_signature="$(watch_signature)"
	note "Watching for source changes"

	while true; do
		sleep 1
		next_signature="$(watch_signature)"
		if [[ "$next_signature" != "$current_signature" ]]; then
			rebuild_and_restart_dev
			current_signature="$(watch_signature)"
		fi
	done
}

start_dev_session() {
	ensure_dirs
	stop_all_sessions
	note "Starting tmux session $DEV_SESSION_NAME"
	local cmd
	cmd="set -euo pipefail; exec ${(q)ROOT_DIR}/self_host.zsh __dev-loop >> ${(q)DEV_LOG_PATH} 2>&1"
	tmuxnew "$DEV_SESSION_NAME" zsh -lc "$cmd"
}

setup_cmd() {
	PUBLIC_URL="$(normalize_public_url "${1:-$DEFAULT_PUBLIC_URL}")"
	parse_public_url "$PUBLIC_URL"
	ensure_dirs
	write_state
	build_binary
	write_env
	write_caddy_block
	reload_caddy
	start_app
	note "Snowflakes available at $PUBLIC_URL"
}

redeploy_cmd() {
	if [[ $# -gt 0 ]]; then
		PUBLIC_URL="$(normalize_public_url "$1")"
		parse_public_url "$PUBLIC_URL"
	else
		load_state
		parse_public_url "$PUBLIC_URL"
	fi
	ensure_dirs
	write_state
	build_binary
	write_env
	write_caddy_block
	reload_caddy
	start_app
	note "Redeployed Snowflakes at $PUBLIC_URL"
}

start_cmd() {
	load_state
	parse_public_url "$PUBLIC_URL"
	write_env
	start_app
	note "Started Snowflakes at $PUBLIC_URL"
}

dev_start_cmd() {
	if [[ $# -gt 0 ]]; then
		PUBLIC_URL="$(normalize_public_url "$1")"
		parse_public_url "$PUBLIC_URL"
	else
		load_state
		parse_public_url "$PUBLIC_URL"
	fi
	ensure_dirs
	write_state
	write_env
	write_caddy_block
	reload_caddy
	start_dev_session
	note "Development Snowflakes available at $PUBLIC_URL"
}

stop_cmd() {
	stop_all_sessions
}

main() {
	local command="${1:-}"
	case "$command" in
		setup)
			require_command go
			require_command templ
			require_command tmux
			require_command caddy
			require_command python3
			setup_cmd "${2:-$DEFAULT_PUBLIC_URL}"
			;;
		redeploy)
			require_command go
			require_command templ
			require_command tmux
			require_command caddy
			require_command python3
			redeploy_cmd "${2:-}"
			;;
		start)
			require_command tmux
			start_cmd
			;;
		dev-start)
			require_command go
			require_command templ
			require_command tmux
			require_command caddy
			require_command python3
			dev_start_cmd "${2:-}"
			;;
		stop)
			require_command tmux
			stop_cmd
			;;
		__dev-loop)
			require_command go
			require_command templ
			require_command python3
			run_dev_loop
			;;
		*)
			usage
			exit 1
			;;
	esac
}

main "$@"
