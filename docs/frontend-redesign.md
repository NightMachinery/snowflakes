# Frontend redesign notes

_Date: 2026-04-23_

## Scope
- Replaced the old Snowflakes frontend from scratch.
- Kept the existing HTTP/game contracts intact so room actions, fragments, SSE refresh, and tests still work.
- Updated the landing page, room shell, stage flow, people rail, settings rail, and supporting frontend JS/CSS.

## Image-first design pass
The redesign followed an image-first workflow using five generated visual references before implementation:
- landing hero + create/join dock
- room lobby / start state
- active round clue-entry state
- review / guess state
- sidebar component detail view

Those references drove the implemented design system: dark winter-night backdrop, aurora accents, frosted surfaces, large readable type, and a cleaner control-room layout.

## Landing refinement (2026-04-23, follow-up)
- Simplified the landing into a single minimalist hero instead of a multi-block marketing layout.
- Merged create/join into one refined frosted action dock.
- Added a more prestigious animated northern aurora treatment for the landing background.
- Kept room pages and gameplay UI intact while making the homepage calmer and more focused.

## New design system
- Dark atmospheric shell with layered aurora glows and a dedicated animated landing sky
- Editorial serif display headlines with clean sans UI text
- Minimal single-hero landing with a shared create/join action dock
- Room banner with share link, viewer identity, and high-level stats
- Round journey strip for choose → clue → review → guess → resolve
- Spacious stage content that keeps hidden-info states obvious
- Cleaner people/settings rail with less nested box noise

## Files changed
- `internal/snowflakes/components.templ`
- `internal/snowflakes/components_templ.go` (regenerated locally by `templ`, ignored by git)
- `internal/snowflakes/render.go`
- `internal/snowflakes/static/styles.css`
- `internal/snowflakes/static/app.js`
- `internal/snowflakes/static/landing-sky.png`
- `self_host.zsh`

## Verification
- `templ generate -path internal/snowflakes`
- `gofmt -w internal/snowflakes/render.go`
- `go test ./...`


## Aurora landing recovery (2026-04-24)
- Restored the landing toward the stronger saved references in `~/.codex/generated_images/` instead of the flatter intermediate version.
- Switched the hero back to a centered editorial composition with a quieter wordmark, more breathing room, and a single refined action dock.
- Added a generated cinematic aurora landscape plate at `internal/snowflakes/static/landing-sky.png` and layered subtle animated aurora motion back over it so the homepage reads as a real northern-sky scene again.
- Kept the room/gameplay shell intact while making sure the room UI still rendered correctly after the landing changes.

## Self-hosting workflow updates (2026-04-24)
- Added `./self_host.zsh dev-start [public-url]` for tmux-managed development rebuilds.
- Made `setup`, `redeploy`, `start`, `dev-start`, and `stop` coordinate so `start` and `dev-start` cleanly replace each other instead of overlapping.
- Fixed the initial watcher bug where generated `*_templ.go` files caused rebuild loops.
- Switched `dev-start` from the custom polling watcher to an Air-backed live-reload session while keeping tmux/Caddy/state management in the wrapper script.

## Verification (2026-04-24)
- `./self_host.zsh dev-start http://justone.pinky.lilf.ir`
- `./self_host.zsh start`
- `./self_host.zsh dev-start http://justone.pinky.lilf.ir`
- `go test ./...`
- Chrome MCP review on the official URL for both landing and room creation flow
