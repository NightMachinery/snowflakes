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

## New design system
- Dark atmospheric shell with layered aurora glows
- Editorial serif display headlines with clean sans UI text
- Large landing hero with direct create/join actions
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

## Verification
- `templ generate -path internal/snowflakes`
- `gofmt -w internal/snowflakes/render.go`
- `go test ./...`
