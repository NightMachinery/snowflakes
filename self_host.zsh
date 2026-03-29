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
CADDYFILE="$HOME/Caddyfile"
SESSION_NAME="snowflakes-self-host"
DEFAULT_PUBLIC_URL="http://justone.pinky.lilf.ir"
APP_HOST="127.0.0.1"
APP_PORT="3400"
CADDY_BEGIN="# BEGIN snowflakes self-host"
CADDY_END="# END snowflakes self-host"
PROXY_EXPORTS='export ALL_PROXY=http://127.0.0.1:20808 all_proxy=http://127.0.0.1:20808 http_proxy=http://127.0.0.1:20808 https_proxy=http://127.0.0.1:20808 HTTP_PROXY=http://127.0.0.1:20808 HTTPS_PROXY=http://127.0.0.1:20808 npm_config_proxy=http://127.0.0.1:20808 npm_config_https_proxy=http://127.0.0.1:20808'

PUBLIC_URL=""
SITE_ADDRESS=""
WORDPACK_DIR="${SNOWFLAKES_WORDPACK_DIR:-$HOME/.snowflakes/wordpacks}"

tmuxnew () {
	tmux kill-session -t "$1" &> /dev/null || true
	tmux new -d -s "$@"
}

usage() {
	cat <<USAGE
Usage:
  ./self_host.zsh setup [public-url]
  ./self_host.zsh redeploy [public-url]
  ./self_host.zsh start
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

load_proxy_env() {
	eval "$PROXY_EXPORTS"
}

write_env() {
	cat > "$ENV_PATH" <<EOF_ENV
PORT=$APP_PORT
NETWORK_ADDRESS=$APP_HOST
ROOT_URL=$PUBLIC_URL
SNOWFLAKES_PUBLIC_URL=$PUBLIC_URL
SNOWFLAKES_WORDPACK_DIR=$WORDPACK_DIR
EOF_ENV
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
	load_proxy_env

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

start_app() {
	ensure_dirs
	[[ -x "$BIN_PATH" ]] || die "Missing $BIN_PATH. Run setup or redeploy first."
	write_env
	note "Starting tmux session $SESSION_NAME"
	local cmd
	cmd="set -euo pipefail; source ${(q)ENV_PATH}; exec ${(q)BIN_PATH} >> ${(q)LOG_PATH} 2>&1"
	tmuxnew "$SESSION_NAME" zsh -lc "$cmd"
}

stop_app() {
	note "Stopping tmux session $SESSION_NAME"
	tmux kill-session -t "$SESSION_NAME" &>/dev/null || true
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

stop_cmd() {
	stop_app
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
		stop)
			require_command tmux
			stop_cmd
			;;
		*)
			usage
			exit 1
			;;
	esac
}

main "$@"
