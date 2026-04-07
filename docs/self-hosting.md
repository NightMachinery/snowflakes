# Self-hosting Snowflakes

## Requirements
- Go 1.25 toolchain support via the standard `go` command (`go` may auto-download Go 1.25 on first build)
- `templ`
- `tmux`
- `Caddy`
- `python3`
- a writable `~/Caddyfile`

## Default URL and runtime
- Default public URL: `http://justone.pinky.lilf.ir`
- Internal bind: `127.0.0.1:3400`
- tmux session: `snowflakes-self-host`
- Runtime files: `.self-host/`
- Extra word packs: `~/.snowflakes/wordpacks/` or `SNOWFLAKES_WORDPACK_DIR`

## Proxy
The self-host script does **not** define a proxy for you.

If your shell already has proxy variables set (for example `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, or `npm_config_*_proxy`), the script passes them through to the build and to the tmux-managed app process.

## Commands
```zsh
./self_host.zsh setup [public-url]
./self_host.zsh redeploy [public-url]
./self_host.zsh start
./self_host.zsh stop
```

### `setup`
- runs `templ generate -path internal/snowflakes`
- builds the current local checkout
- writes runtime env/state files
- adds or replaces the managed `Snowflakes` block in `~/Caddyfile`
- reloads Caddy
- starts the app in tmux

### `redeploy`
- reruns `templ generate -path internal/snowflakes`
- rebuilds the current local checkout
- rewrites the managed Caddy block if a new URL is provided
- reloads Caddy
- restarts the tmux session

### `start`
- starts the last configured build/env without rebuilding

### `stop`
- stops the tmux session

## Notes
- URLs must be host-only (`http://host` or `https://host`). Path prefixes are not supported.
- No Docker is used.
- `redeploy` deploys the latest **local** changes; it does not fetch or pull anything.
- Generated `templ` Go files are not committed; local builds and deploys generate them on demand.
