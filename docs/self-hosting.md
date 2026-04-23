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
- tmux app session: `snowflakes-self-host`
- tmux dev session: `snowflakes-self-host-dev`
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
./self_host.zsh dev-start [public-url]
./self_host.zsh stop
```

### `setup`
- runs `templ generate -path internal/snowflakes`
- builds the current local checkout
- writes runtime env/state files
- adds or replaces the managed `Snowflakes` block in `~/Caddyfile`
- reloads Caddy
- stops any existing `start` or `dev-start` session before launching the app

### `redeploy`
- reruns `templ generate -path internal/snowflakes`
- rebuilds the current local checkout
- rewrites the managed Caddy block if a new URL is provided
- reloads Caddy
- stops any existing `start` or `dev-start` session before launching the app

### `start`
- starts the last configured build/env without rebuilding
- stops any running `start` or `dev-start` session first

### `dev-start`
See also: [`docs/dev-start.md`](./dev-start.md) for the current implementation details and alternatives.

- reloads Caddy for the configured public URL
- launches a tmux-managed development watcher
- rebuilds on changes to Go, templ, CSS, JS, embedded word pack text, SVG, and the self-host script itself
- restarts the app automatically after a successful rebuild
- ignores generated `*_templ.go` outputs so the watcher does not rebuild-loop on its own generated files
- stops any running `start` or `dev-start` session first

### `stop`
- stops both the normal app session and the dev watcher session

## Notes
- URLs must be host-only (`http://host` or `https://host`). Path prefixes are not supported.
- No Docker is used.
- `redeploy` deploys the latest **local** changes; it does not fetch or pull anything.
- Generated `templ` Go files are not committed; local builds and deploys generate them on demand.
- On this VPS, run the manual machine-health preflight before build-like commands (`setup`, `redeploy`, `dev-start`, `go test`, etc.). Those checks intentionally stay outside the script.
