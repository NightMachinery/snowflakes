# `dev-start` internals

_Date: 2026-04-24_

This documents how the current `./self_host.zsh dev-start [public-url]` implementation works.

## Short version

`dev-start` is a small custom remote-dev wrapper around four jobs:

1. remember the public URL and runtime env
2. rewrite/reload the managed Caddy block
3. start a dedicated tmux watcher session
4. rebuild + restart the Snowflakes binary whenever watched files change

It is **not** browser hot-module reload. It is a tmux-managed **rebuild/restart loop** for a Go app that is reachable through the configured public Caddy URL.

## Entry point

The command is dispatched from `self_host.zsh`:

- `dev-start` command case: `self_host.zsh:406-412`
- main command body: `self_host.zsh:362-376`
- watcher session launcher: `self_host.zsh:314-320`
- internal watcher loop: `self_host.zsh:290-312`

## What happens when you run it

### 1. Choose the public URL

`dev_start_cmd()` either:

- normalizes the CLI argument with `normalize_public_url()` (`self_host.zsh:63-70`), or
- loads the last saved config from `.self-host/state.env` via `load_state()` (`self_host.zsh:143-146`).

`parse_public_url()` then rejects anything except host-only `http://...` or `https://...` URLs. Path prefixes are explicitly unsupported (`self_host.zsh:72-92`).

### 2. Prepare runtime state

It creates runtime directories under `.self-host/` (`self_host.zsh:94-96`) and writes:

- `.self-host/state.env` with `PUBLIC_URL`, `SITE_ADDRESS`, `WORDPACK_DIR` (`self_host.zsh:135-141`)
- `.self-host/app.env` with bind/env settings plus inherited `*_PROXY` variables (`self_host.zsh:98-133`)

That means SSH proxy env and the chosen public URL survive into the dev child process.

### 3. Reconfigure Caddy

`dev-start` renders a managed Caddy block for the selected site address and writes it into `~/Caddyfile` between:

- `# BEGIN snowflakes self-host`
- `# END snowflakes self-host`

Relevant code:

- render block: `self_host.zsh:170-176`
- replace block in `~/Caddyfile`: `self_host.zsh:179-204`
- reload Caddy: `self_host.zsh:206-209`

So the public hostname continues to point at `127.0.0.1:3400` even while the dev watcher is cycling the app process.

### 4. Replace any existing app/dev session

Before starting, it kills both tmux sessions:

- normal app session: `snowflakes-self-host`
- dev watcher session: `snowflakes-self-host-dev`

Code: `self_host.zsh:211-215`, called from `start_dev_session()` at `self_host.zsh:314-320`.

This is why `start` and `dev-start` cleanly replace each other instead of overlapping.

### 5. Start the dedicated tmux watcher session

`start_dev_session()` launches:

```zsh
./self_host.zsh __dev-loop >> .self-host/logs/snowflakes-dev.log 2>&1
```

inside tmux session `snowflakes-self-host-dev` (`self_host.zsh:314-320`).

So `dev-start` itself returns quickly; the real work continues in tmux.

## What `__dev-loop` does

`run_dev_loop()` is the long-running development supervisor (`self_host.zsh:290-312`).

### Initial boot

On startup it:

1. reloads saved state
2. reparses the public URL
3. rewrites `app.env`
4. installs a trap to kill the child app on exit
5. immediately calls `rebuild_and_restart_dev()`

### Rebuild/restart behavior

`rebuild_and_restart_dev()` (`self_host.zsh:278-288`) does:

1. `build_binary()`
2. on success, rewrite env
3. stop the current child app
4. launch the new binary in the background
5. keep the watcher loop alive

If the rebuild fails, it logs the failure and **keeps the previous app process running**.

That detail is important: a bad save does not necessarily take the already-running dev server down.

## What a rebuild actually does

`build_binary()` (`self_host.zsh:148-168`) is a full local rebuild:

1. run `templ generate -path internal/snowflakes`
2. run `GOWORK=off go build -buildvcs=false -o .self-host/bin/snowflakes ./cmd/snowflakes`

So the dev loop always produces a fresh local binary at `.self-host/bin/snowflakes` before restart.

## How file watching works today

The watcher is intentionally simple.

`watch_signature()` (`self_host.zsh:228-262`) computes a SHA-256 over each watched file's:

- relative path
- `mtime_ns`
- file size

The loop then:

- sleeps 1 second
- recomputes the signature
- rebuilds if the signature changed

Code: `self_host.zsh:300-310`.

### Files that are watched

Root:

- `go.mod`
- `go.sum`
- `self_host.zsh`

Recursive under `cmd/` and `internal/`:

- `.go`
- `.templ`
- `.css`
- `.js`
- `.txt`
- `.svg`

### Important exclusion

Generated `*_templ.go` files are skipped (`self_host.zsh:249-250`).

That avoids the classic loop:

1. `.templ` changes
2. `templ generate` writes `*_templ.go`
3. watcher sees generated file change
4. watcher rebuilds again forever

## Process model

There are really two processes in dev mode:

1. **supervisor**: tmux session running `__dev-loop`
2. **child app**: background `zsh -lc "source .self-host/app.env; exec .self-host/bin/snowflakes"`

Relevant code:

- stop child: `self_host.zsh:264-270`
- start child: `self_host.zsh:272-276`

The child app logs to `.self-host/logs/snowflakes.log`.
The watcher loop logs to `.self-host/logs/snowflakes-dev.log`.

## What this implementation is good at

The custom script is useful because it combines several repo-specific needs in one command:

- tmux persistence on a remote VPS
- public Caddy URL management
- env persistence in `.self-host/`
- proxy variable pass-through
- `templ` generation before every build
- coordination with `setup`, `redeploy`, `start`, and `stop`

That bundle is the real reason the script exists.

## Limitations of the current implementation

Compared with mature watch/reload tools, this version is intentionally bare-bones:

- polling-based watch loop, not event-driven
- 1-second detection granularity
- full rebuild on any watched change
- no browser auto-reload
- no CSS/JS hot reload
- no graceful shutdown protocol beyond killing the child
- only watches `cmd/`, `internal/`, and a few root files
- no ignore file support like `.gitignore`
- no debounce/coalescing beyond the 1-second poll interval

## Are there already mature solutions for this use case?

Yes — for the **watch/rebuild/restart** part, absolutely.

As of **2026-04-24**, the closest mature options are:

1. **templ live reload**  
   Official docs show `templ generate --watch --proxy="http://localhost:8080" --cmd="go run ."`, which covers templ regeneration, server restart, and browser reload out of the box.
2. **Air**  
   A widely used Go live-reload tool with configurable build/run commands, exclude rules, `.env` loading, and an optional proxy for browser reload.
3. **Reflex**  
   A smaller generic watcher that can restart long-running services with `-s` and can express this workflow with shell commands.
4. **watchexec**  
   A mature cross-language watcher that restarts commands on file changes and handles ignore files/process management well.

### So why keep a custom `dev-start`?

Because the repo is not solving only “watch files and rerun Go.” It is also solving:

- “keep the app alive in tmux on a remote VPS”
- “remember and apply the public URL”
- “keep Caddy in sync”
- “persist runtime env and wordpack location”
- “make `start` and `dev-start` replace each other cleanly”

Mature tools already exist for the watcher/reloader core, but **this script is a workflow wrapper around deployment-ish concerns**.

## Best current interpretation

So the honest answer is:

- **Yes**, mature solutions already exist for most of the inner loop.
- **No**, this script is not pure duplication, because it also owns tmux + Caddy + persisted self-host state.

If we wanted to simplify later, the cleanest direction would likely be:

- keep this repo-specific wrapper for tmux/Caddy/env/state, but
- replace the homemade polling watcher with `templ --watch`, Air, Reflex, or watchexec.

## Why not just use `templ generate --watch`?

We probably **could**, but it is not a drop-in replacement for the current workflow.

The official `templ` live-reload flow is aimed at local development: it watches `*.templ` and `*.go`, can restart a command, and serves a browser-reload proxy on `localhost:7331` by default.

For this repo, the gaps are mostly around workflow ownership rather than templ itself:

- current `dev-start` also manages **tmux**
- it rewrites and reloads the repo's managed **Caddy** block
- it persists the chosen **public URL** and runtime env in `.self-host/`
- it coordinates with `setup`, `start`, `redeploy`, and `stop`
- it currently rebuilds on some non-Go/non-templ assets too (`.css`, `.js`, `.txt`, `.svg`)

So the real answer is not “templ can't do live reload” — it clearly can. The answer is “templ live reload solves the inner dev loop, while this script currently also owns the surrounding remote self-host workflow.”

If we simplify later, the most likely good direction is:

1. keep the repo-specific wrapper for tmux/Caddy/state/env
2. replace the homemade polling watcher with `templ generate --watch`
3. optionally let Caddy front the templ proxy, or switch dev mode to a direct templ proxy URL

## External references

- templ live reload docs: <https://templ.guide/developer-tools/live-reload/>
- Air: <https://github.com/air-verse/air>
- Reflex: <https://github.com/cespare/reflex>
- watchexec: <https://github.com/watchexec/watchexec>
