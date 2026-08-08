# Hourglass — Self-Hosted Crontab Manager

A lightweight, single-binary Go application for viewing and managing Linux/macOS crontab jobs from a web UI, with an MCP server for AI-agent access and SSH-remote connection management. Hourglass is a **shipped product, not a prototype**: it ships LAN-first zero-config defaults (mDNS + auto-generated credentials), local HTTPS for `hourglass.local`, automatic execution-history tracking, and CI/CD that publishes binaries on every version bump.

**Stack:** Go 1.21+ (stdlib `net/http` only), vanilla HTML/CSS/JS (`ui/src` → `ui/dist` build step), Linux cron / macOS crontab via `os/exec`.
**Deployment:** single ~8 MB binary; .deb/.rpm packages with a systemd unit; Homebrew tap for macOS; GitHub Actions auto-release.
**Repo:** https://github.com/TillmanBuildsTech/hourglass

## Repository Layout

```
├── main.go                 # Entrypoint: flags, wiring, routes, HTTP handlers
├── auth.go                 # Session-cookie auth: public shell, gated API, middleware
├── bind_security.go        # Auto-generated credentials + bind security enforcement
├── tls.go                  # Local HTTPS: per-machine root CA for *.local names
├── mdns.go                 # mDNS advertisement of hourglass.local (+ conflict auto-rename)
├── redirect.go             # Same-port HTTP→HTTPS 308 redirect (sniffListener)
├── bonjour_darwin.go       # macOS native Bonjour registration (cgo) — stub elsewhere
├── cron/
│   ├── parser.go           # crontab text ⇄ Entry structs (validation, env lines, comments)
│   ├── manager.go          # read/write/execute via the Executor; history wrapping
│   └── history.go          # history.log parsing (RFC3339, exit code, status)
├── ssh/client.go           # SSH executor — remote crontabs over SSH
├── connection/config.go    # Saved connections (connections.json)
├── mcp/                    # --mcp stdio MCP server (JSON-RPC 2.0, no SDK)
├── ui/
│   ├── index.html          # SPA shell (embedded via go:embed)
│   ├── src/                # source JS/CSS/icons — EDIT HERE
│   └── dist/               # built output (gitignored; CI runs `npm run build`)
├── e2e/                    # Playwright suite (isolated via FileExecutor)
├── packaging/              # systemd unit, env file, package scripts
└── VERSION                 # embedded version (see Versioning)
```

## The Core Invariant: Everything Goes Through the Executor

`cron.Manager` operates on an `Executor` interface (`cron/manager.go`):

- **Local:** `LocalExecutor` runs `crontab` via `os/exec` (honors `HOURGLASS_CRONTAB_USER` for multi-user targeting and `HOURGLASS_CRONTAB_FILE` for isolated tests).
- **Remote:** the `ssh.Client` IS an executor. Switching connections (or restoring the saved active one at boot) calls `cronManager.SetExecutor(client)`.

The manager reads/writes the crontab AND history through the current executor — that is what makes SSH-remote connections transparent. **Any new feature that does its own `os/exec`/`os.ReadFile` on the local machine is broken for remote connections.** Touch the crontab, the history log, or anything on the job host only through the executor (or a new `Manager` method that does).

## Execution History

- `WriteCrontab` wraps every active command so that at run time it appends `<unix-millis>\t<exit-code>\t<base64(cmd)>` to `~/.hourglass/history.log` **on the host that runs the job** (local or remote — `$HOME` is resolved on the job host).
- `HistoryCache.Refresh(executor)` re-reads that file through the executor (for remote: SSH + `cat`). Runs on a 30s ticker and lazily on `GET /api/cron` when stale.
- `GET /api/logs` decodes the raw records into RFC3339 timestamps + exit code + status + decoded command (newest first) via `cron.ParseHistoryLog`.
- Jobs installed outside Hourglass aren't wrapped; `handleGetCron` auto-wraps untracked active jobs on first read (best-effort write).

## LAN-First Defaults (v0.13+)

- **Default bind is `0.0.0.0:8080`** so `hourglass.local` works out of the box from any device. Loopback is an explicit `HOURGLASS_BIND=127.0.0.1:8080`.
- **Auto-generated credentials:** on a non-loopback bind with no `HOURGLASS_AUTH_USER/PASS`, a 16-hex-char password is generated, persisted to `~/.hourglass/auth.env` (0600), and printed at startup. Loopback binds stay auth-free.
- **mDNS by default** advertises `hourglass.local` with probe-before-announce; on a name conflict it auto-renames to `<name>-2.local`, `<name>-3.local`, … (bounded). macOS brew/cgo builds register directly with `mDNSResponder` (Bonjour); Linux/Windows/CGO_ENABLED=0 builds use the built-in multicast responder.
- **Local HTTPS:** a per-machine root CA (`~/.hourglass/tls/`) is generated and installed into the OS trust store so `https://hourglass.local` shows a valid lock (public CAs can't issue for `.local`). `HOURGLASS_TLS=auto` (default) falls back to plain HTTP if the CA can't be installed. Plain-HTTP requests to the TLS port get a 308 redirect to `https://` — `http://localhost:8080` just works. `/ca.pem` serves the public root CA so other LAN devices can trust it.

## Auth Model (v0.11+)

- **Public shell, gated API:** `/`, `/dist/*`, `/api/auth/*`, `/api/version`, and `/ca.pem` are public; every other `/api/*` route requires a valid session.
- Session tokens are stateless HMAC-SHA256 cookies (`hg_session`, 7-day, HttpOnly, SameSite=Lax); the key persists at `~/.hourglass/auth.key` so logins survive restarts. Logout revokes server-side via an in-memory revocation map (a replayed cookie must not authenticate). Basic Auth remains a working fallback for curl/scripts.
- The frontend shows an in-app login view on 401 — **never** send `WWW-Authenticate: Basic` on API 401s (it re-triggers the browser's native dialog).

## REST API

| Route | Methods | Notes |
|---|---|---|
| `/api/cron` | GET / POST / DELETE | list / add / delete jobs (GET may auto-wrap untracked jobs) |
| `/api/cron/update` | POST | edit a job |
| `/api/cron/toggle` | POST | enable / disable |
| `/api/cron/execute` | POST | run a job now (synchronous) |
| `/api/logs` | GET | decoded execution history |
| `/api/connections` | GET / POST / DELETE | saved connections |
| `/api/connections/active` | POST | switch the active connection |
| `/api/connections/test` | POST | reachability check |
| `/api/auth/login` `/api/auth/me` `/api/auth/logout` | POST / GET / POST | session auth |
| `/api/version` | GET | version + GOOS |
| `/ca.pem` | GET | public root CA |
| `/` `/dist/*` | GET | SPA shell + static assets |

## CLI

- `hourglass` — web server
- `hourglass --version` — print version, exit
- `hourglass --mcp` — stdio MCP server (5 tools: list / create / update / delete cron jobs, validate schedule). Backed by the same `cron.Manager`, so the active connection, `HOURGLASS_CRONTAB_USER`, and `HOURGLASS_CRONTAB_FILE` all apply.
- `hourglass -install-ca` — generate the local TLS CA (if needed) and install it into the OS trust store, then exit.

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `HOURGLASS_BIND` | `0.0.0.0:8080` | Bind address. LAN-reachable by default so `hourglass.local` works; `127.0.0.1:8080` for loopback-only |
| `HOURGLASS_AUTH_USER` / `HOURGLASS_AUTH_PASS` | (auto) | Credentials. Auto-generated + saved to `~/.hourglass/auth.env` on non-loopback binds when unset |
| `HOURGLASS_ALLOW_INSECURE` | (none) | `1` serves without auth on non-loopback binds (dangerous — explicit opt-in) |
| `HOURGLASS_MDNS` | `1` | Advertise `hourglass.local` (skipped on loopback binds; `0` disables) |
| `HOURGLASS_MDNS_NAME` | `hourglass` | The `<name>` in `<name>.local` |
| `HOURGLASS_TLS` | `auto` | Local HTTPS mode: `auto`, `1` (force HTTPS), `0` (plain HTTP) |
| `HOURGLASS_CRONTAB_USER` | (none) | Manage a specific user's crontab via `crontab -u <user>` (requires root) — for root-run services whose jobs live under another account |
| `HOURGLASS_CRONTAB_FILE` | (none) | Redirect crontab I/O to a plain file — **testing only** (isolates E2E from the real system crontab) |

## Code Conventions

### Go
- **Error handling:** always wrap system errors with context, log to stderr.
- **Naming:** short names, no abbreviations (`parseCrontab`, not `parseCron`).
- **Testing:** each major function gets a test file (`*_test.go`). New methods get a test. Run `go test ./... -count=1`.

### Frontend
- **A build step exists:** edit `ui/src/`, then `cd ui && npm run build` (terser + tailwindcss CLI → `ui/dist/`). `ui/dist/` is gitignored build output — never commit it; CI builds it.
- Vanilla JS, no framework. State kept in memory, synced to the API. Semantic HTML + ARIA labels.

### Commit Messages
`type(scope): description` — `feat(cron): …`, `fix(api): …`, `docs: …`, `test(parser): …`.

## Versioning

`VERSION` file at the repo root, embedded via `go:embed`, exposed through `--version`, `GET /api/version`, and the UI header. **Bump it with every code PR** — patch (`0.14.x`) for fixes/small changes, minor (`0.x.0`) for new features, major (`x.0.0`) for breaking changes. Merging a VERSION bump to `main` triggers the auto-release workflow (tag → binaries/packages → Homebrew formula update). **Docs-only PRs should NOT bump VERSION** — that ships an empty release.

## Testing

- **Unit:** parser edge cases, manager read/write flows (fake `crontab` on PATH), history parsing, auth/session, TLS cert lifecycle, mDNS wire payloads, redirect behavior, MCP handlers (in-memory executor). Current coverage: `cron` ~80%, `ssh` ~88%, `connection` ~94%, `mcp` ~83%.
- **E2E (Playwright, `e2e/`):** `bash e2e/run.sh`. Isolated from the real system crontab via `HOURGLASS_CRONTAB_FILE` (FileExecutor) + scratch `HOME` + loopback bind + `HOURGLASS_MDNS=0 HOURGLASS_TLS=0`.
- **CI (`build.yml`):** `go test -v -cover ./...` on linux + macOS × Go 1.21/1.22 (macOS runs with cgo enabled — the only place the Bonjour cgo code compiles), then cross-compiles linux/darwin × amd64/arm64. `auto-release.yml` tags + releases on VERSION bumps to `main`.

## Known Limitations

- **Single admin** — no locking; concurrent edits can overwrite each other (documented, first-write-wins).
- **macOS** — crontab is fully supported; native `launchd` job management is a planned future feature.
- **Windows** — unsupported (no crontab).
- **Managed-jobs history only** — jobs added via `crontab -e` aren't wrapped until saved through Hourglass.
- **No clustering** — each instance manages its own crontab.

## Roadmap

The public roadmap lives in README.md; open items include exposed-instance hardening (rate limiting, CSRF), structured logging + graceful shutdown, a deeper `/health` endpoint, backup/restore, native launchd support, and an interactive schedule builder. See the README Roadmap section for the full list.
