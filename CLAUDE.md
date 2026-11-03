# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Sloubi.chat: multi-room real-time chat (Go + Gin + SQLite) with a vanilla-JS PWA frontend. Each room lives at `sloubi.chat/<chan>` and has its own collaboratively counted "sloubi" number and a shared post-it. The original single room is `/chante`; `/` is the home page where you type a room name. Code comments, UI text, README and TODO are in French; keep that convention.

## Commands

```bash
bin/build.sh                # PRODUCTION deploy: tidy, vet, build, govulncheck, then restart the live systemd service `sloubi`
go run .                    # run locally (127.0.0.1:8888; PORT=..., HOST=0.0.0.0 to expose)
go test -race ./...         # all tests live in src/web/web_test.go
go test ./src/web -run TestRoutes   # single test
go vet ./...
govulncheck ./...           # dependency audit (go install golang.org/x/vuln/cmd/govulncheck@latest)
```

The project targets Go 1.27.3. There is no linter config, no JS build step and no Dockerfile.

The server resolves `assets/`, `views/`, `db/` and `forbidden.json` relative to the **current working directory**, so run it from the repo root (tests `chdir` there in `TestMain`). It refuses to start without a valid `forbidden.json`. `db/chat.sqlite` (WAL mode) is created and migrated on startup and is gitignored.

Production: systemd unit `sloubi` runs `./sloubi` from the repo root (hardening drop-in in `deploy/hardening.conf`: `www-data`, read-only FS except `db/`; anything new that writes to disk must go under `db/` or get its own `ReadWritePaths`), behind Caddy (`/etc/Caddyfile`, reverse proxy to 127.0.0.1:8888). Gin trusts `X-Forwarded-For` only from 127.0.0.1/::1. Rate limits key on `clientKey` (IPv4, or the IPv6 /64).

## Architecture

- `src/web/routes.go` holds the `server` struct (store, hub, forbidden list, one `Limiter` per action) and all routes:
  - Pages: `GET /` renders `views/home.html`. `GET /:chan` renders `views/room.html` (the 3D smoky tavern) with `{{.Chan}}` for every room, `chante` included. Uppercase names get a 301 to lowercase; invalid or forbidden names get a 404 that renders the home page with the name prefilled, so it shows red.
  - API: `/api/check/:chan`, `POST /api/:chan/messages`, `GET /api/:chan/last-sloubi`, `POST /api/:chan/postit`, `GET /ws/:chan`.
  - Handlers take a `channelResolver`, so the legacy routes (`/verif.json`, `/last-sloubi.json`, `/ws`) reuse the same code, pinned to `chante`.
  - Input hygiene happens here: `application/json` required, 4 KB body cap, length limits. `cleanText` applies NFC, strips control and format characters (bidi, zero-width; ZWJ kept) and caps combining marks at 2.
  - Transport: `http.Server` timeouts (5 s headers), `floodGuard` on every request, a WS cap per key (`maxWSPerKey`), and security headers including a CSP with no inline styles. Use the `hidden` attribute or CSSOM (`el.style.x = …`), never `style="…"` in templates.
  - Nicknames go through `cleanNickname`: NFC, Latin script + ASCII digits + space `_-.'` only, 2–24 runes, `forbidden.json` applied. Invalid characters are rejected, not stripped. `POST /api/nick` exposes this for `/nick`; on message posts, an invalid nick falls back to `Anonyme`.
  - A room is created on its first message or post-it, never on a visit. Creation goes through its own rate limit (`mayCreate`).
- `src/web/channels.go`: the room-name regex `^[a-z0-9]+([._-][a-z0-9]+)*$` (max 32 chars) and the `forbidden.json` matcher.
  - Names are normalized before matching: lowercase, separators removed, leet mapped to letters. Then come the `exact` / `allow` / `contains` lists.
  - The list hot-reloads on mtime change; a broken file keeps the previous list.
  - A name containing `.` like `app.js` passes the regex, so every root-level route or file must also be listed in `forbidden.json` under `exact`.
- `src/web/db.go`: `Store` and the migrations (adding `channel` to legacy `messages` with default `chante`, plus the `channels` and `postits` tables).
  - **Sloubi rule:** a message counts only if it matches `sloubi <N>`, N is the room's max + 1, and the `clientId` differs from the previous valid sloubi's author.
  - The check and the insert run under `Store.wmu` inside a transaction, backed by `UNIQUE(channel, sloubi_number)`.
- `src/web/message.go`: the `Hub` keeps rooms → clients. Each client has a buffered `send` channel, and only its `writePump` writes to the conn (gorilla-style conns aren't concurrency-safe).
  - `join` pushes the backlog and the post-it under the hub lock, so clients must dedupe messages by `id`.
  - A full buffer drops the client.
  - WS payloads are `{type:"message", ...flat message fields}` or `{type:"postit", ...}`. `chat.Message.ClientID` is `json:"-"` and is never broadcast.
- `src/web/limiter.go`: a per-IP token bucket with an optional strike-based temporary ban (used for `/api/check`).
- Frontend:
  - `assets/app.js` (room page): reads the room from `body[data-chan]`, posts and connects per room, and implements the post-it. Post-it position and open state live in `localStorage` (`sloubi_postit_<chan>`); its text is shared and last write wins.
  - `/nick <pseudo>` is handled client-side in `sendMessage` (nothing gets posted). The client stores the canonical name the server returns.
  - `assets/room-fx.js` + `room.css`: a Three.js scene (vendored at `assets/vendor/three/`, MIT; `three.module.js` imports `./three.core.js`). Procedural textures only, no image assets.
    - It reacts to the `sloubi:message` window event that `app.js` dispatches per rendered message (`detail.live` is false for backlog). Live messages puff smoke; live sloubis fire a spark burst and add `.flare` to the counter.
    - It degrades on slow frames, renders a single static frame under reduced motion, and falls back to the CSS background when WebGL is unavailable (`body.no-webgl`).
    - Headless testing needs Playwright's `chrome-headless-shell` with `--use-angle=swiftshader --enable-unsafe-swiftshader`; system Chromium gets no WebGL there.
  - `assets/home.js` + `home.css` (home page): local validation, then a debounced `/api/check`. The `body[data-state]` value (`ok` / `bad` / `wait` / `empty`) drives the emerald and lava 8-bit styling.
  - The CSP is strict (`script-src 'self'`, `font-src 'self'`), so no inline scripts: pass server data through `data-*` attributes. Fonts are self-hosted in `assets/fonts/`; don't add third-party CSS or fonts.
  - The room page closes its WebSocket on `pagehide` and reconnects on `pageshow` (bfcache). Without that, sockets survive navigation and stall the browser for seconds. Keep this when touching the WS code.

## Roadmap

`TODO.md` (in French) tracks what's done and what's left: purging inactive rooms, a per-room PWA, removing the committed binary from git, and the Dockerfile decision.
