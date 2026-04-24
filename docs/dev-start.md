# `dev-start` internals

_Date: 2026-04-24_

This documents the current `./self_host.zsh dev-start [public-url]` implementation after the Air migration.

## Short version

`dev-start` is now a repo-specific remote-dev wrapper around:

1. remembering the public URL and runtime env
2. rewriting/reloading the managed Caddy block
3. starting a dedicated tmux session
4. delegating file watch, rebuild, app restart, and browser reload to Air

The custom part is still the tmux/Caddy/state workflow. The live-reload engine is now Air.

## What happens when you run it

### 1. Choose and persist the public URL

`dev-start` either:

- normalizes the CLI argument, or
- reloads the last saved URL from `.self-host/state.env`

It still only accepts host-only `http://...` or `https://...` URLs. Path prefixes are unsupported.

It then writes:

- `.self-host/state.env` with `PUBLIC_URL`, `SITE_ADDRESS`, and `WORDPACK_DIR`
- `.self-host/app.env` with bind/env settings plus inherited `*_PROXY` variables

That means the Air session and the Snowflakes app inherit the same persisted runtime env as before.

### 2. Repoint Caddy at Air's dev proxy

In normal mode, Caddy proxies to the app on `127.0.0.1:3400`.

In dev mode, `dev-start` rewrites the managed Caddy block so the public hostname points to Air's proxy on `127.0.0.1:3401` instead. Air then forwards to the Snowflakes app on `127.0.0.1:3400`.

This is what makes browser auto-reload possible without changing the public URL workflow.

### 3. Replace any existing app/dev session

Like before, `dev-start` kills both tmux sessions first:

- `snowflakes-self-host`
- `snowflakes-self-host-dev`

That keeps `start` and `dev-start` mutually exclusive.

### 4. Launch Air inside tmux

The dev tmux session now runs:

```zsh
cd /path/to/repo
set -a
source .self-host/app.env
set +a
go tool air -c .air.toml
```

The tmux/Air supervisor logs go to `.self-host/logs/snowflakes-dev.log`.

The Snowflakes app itself still logs to `.self-host/logs/snowflakes.log`.

## What Air is responsible for

Air now owns the inner dev loop:

- watch files
- regenerate templ output
- rebuild the Snowflakes binary
- restart the app when the build succeeds
- keep the last good app running when the build fails
- inject browser auto-reload through its proxy

The config lives in `.air.toml`.

## Current Air configuration

### Build command

Air rebuilds with:

```zsh
templ generate -path internal/snowflakes && \
GOWORK=off go build -buildvcs=false -o ./.self-host/bin/snowflakes ./cmd/snowflakes
```

So the dev binary path is unchanged: `.self-host/bin/snowflakes`.

### Entrypoint

Air launches the app through:

```zsh
zsh -lc 'exec ./.self-host/bin/snowflakes >> ./.self-host/logs/snowflakes.log 2>&1'
```

Because the tmux session exports `.self-host/app.env` before starting Air, the app inherits the configured host, port, public URL, wordpack directory, and proxy variables.

### Watched inputs

Air watches:

- `cmd/`
- `internal/`
- `go.mod`
- `go.sum`
- `self_host.zsh`

with extensions:

- `.go`
- `.templ`
- `.css`
- `.js`
- `.txt`
- `.svg`

### Important exclusion

Generated `*_templ.go` files are excluded via regex, so templ generation does not create a rebuild loop.

## Ports in dev mode

- Snowflakes app: `127.0.0.1:3400`
- Air proxy: `127.0.0.1:3401`
- Public URL: whatever host-only URL was configured in `dev-start`

So the request path is:

```text
browser -> public Caddy URL -> Air proxy :3401 -> Snowflakes app :3400
```

## Switching back to normal mode

`./self_host.zsh start` now rewrites and reloads the managed Caddy block back to `127.0.0.1:3400` before launching the normal app tmux session.

That prevents a stale Caddy config from still pointing at Air's proxy after leaving dev mode.

## Why this is better than the previous custom watcher

The old implementation used a homemade polling loop that hashed watched files once per second and manually killed/restarted the child app.

The new setup keeps the useful repo-specific wrapper, but replaces the fragile inner watcher with a mature tool that already handles:

- file watching
- rebuild orchestration
- app restart
- browser reload proxying

## External references

- Air README: <https://github.com/air-verse/air>
- Air example config: <https://raw.githubusercontent.com/air-verse/air/master/air_example.toml>
