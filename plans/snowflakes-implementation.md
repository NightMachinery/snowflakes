# Snowflakes implementation plan

## Summary
- Single Go binary with embedded templates, CSS, JS, and default word pack.
- Server-rendered HTML plus SSE refreshes for live updates.
- In-memory room/game state with cookie + localStorage token identity.
- Self-host with tmux + Caddy, no Docker.

## Core architecture
- `cmd/snowflakes` runs the HTTP server.
- `internal/snowflakes` holds room state, round/game logic, rendering, HTTP handlers, and word-pack parsing.
- Assets and templates are embedded from `internal/snowflakes/static`, `internal/snowflakes/templates`, and `internal/snowflakes/wordpacks`.

## Gameplay behavior
- 13-card cooperative rounds with blind-slot or player-vote word selection.
- Mid-game joins, observer/player/admin controls, exact-match duplicate invalidation, manual invalidation by admins, multi-guesser modes, and configurable clue-slot rules.
- Newline-separated custom word packs from embedded default, room uploads/paste, or `SNOWFLAKES_WORDPACK_DIR`.

## Deployment deliverables
- `docs/self-hosting.md`
- `self_host.zsh [setup|redeploy|start|stop]`
- Managed Caddy block in `~/Caddyfile`
