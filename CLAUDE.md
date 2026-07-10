# Hourglass — Developer Guide

**Project:** Self-hosted web UI for Linux crontab management  
**Language:** Go 1.21+  
**Scope:** Single binary, <15MB, zero external deps

---

## Quick Start

### Build
```bash
go build -o hourglass .
```

### Run (local)
```bash
./hourglass
# Opens on http://localhost:8080
```

### Run (remote)
```bash
HOURGLASS_BIND=0.0.0.0:8080 \
HOURGLASS_AUTH_USER=admin \
HOURGLASS_AUTH_PASS=secretpass \
./hourglass
```

---

## Project Layout

```
.
├── main.go                # HTTP server, routing, embedded FS
├── cron/
│   ├── parser.go         # Parse & validate crontab format
│   ├── parser_test.go    # Parser tests
│   ├── manager.go        # Read/write crontab via os/exec
│   └── manager_test.go   # Manager tests
├── ui/
│   └── index.html        # Single-file frontend (embedded)
├── go.mod                # Module definition
├── CLAUDE.md             # This file
├── Design.md             # Full architecture & decisions
├── TODO.md               # Implementation checklist
└── README.md             # User-facing guide (post-launch)
```

---

## Core Components

### `cron/parser.go`

Parses crontab text to Entry structs. Validates schedule format (minute 0-59, hour 0-23, etc.).

**Key functions:**
- `ParseCrontab(text string) ([]Entry, error)` — Parse text to entries
- `ValidateSchedule(schedule string) error` — Validate "0 9 * * *" format
- `StringifyCrontab(entries []Entry) string` — Convert back to text

### `cron/manager.go`

Interfaces with Linux crontab via os/exec. Handles read/write with error recovery.

**Key functions:**
- `ReadCrontab() (string, error)` — Execute `crontab -l`
- `WriteCrontab(entries []Entry) error` — Execute `crontab -` with stdin

### `main.go`

HTTP server with 3 endpoints. Embeds `ui/index.html` in binary.

**Endpoints:**
- `GET /` — Serve embedded HTML
- `GET /api/cron` — Return JSON array of cron jobs
- `POST /api/cron` — Accept new entries, validate, write to system

### `ui/index.html`

Single HTML file with inline CSS (Tailwind CDN) and vanilla JS.

**Features:**
- Table view of all cron jobs
- Forms to add/edit/delete jobs
- Error message display
- Responsive design (mobile-friendly)

---

## Key Decisions

**See `Design.md` for full rationale.**

| Decision | Choice | Trade-off |
| --- | --- | --- |
| **Concurrency** | No locking (first-write-wins) | Data loss if 2 admins edit simultaneously |
| **Platform** | Linux-only MVP | macOS support deferred |
| **Permissions** | Current user only | Can't edit other users' crontabs |
| **Frontend** | Single HTML + Tailwind CDN | No build step required |
| **API versions** | `/api/cron` (no versioning yet) | Refactor to `/v1/` when features expand |
| **Write safety** | Read-before-write fallback | Extra system call (acceptable) |

---

## Testing

### Run all tests
```bash
go test ./...
```

### Run with coverage
```bash
go test -cover ./...
```

### Integration test
```bash
# Starts server, tests API
go test -run Integration ./...
```

**Target coverage:** 87% (code paths) + 80% (user flows)

See `TODO.md` for detailed test plan.

---

## Code Conventions

- **Error handling:** Wrap system errors with context. Log to stderr.
- **Naming:** Explicit > clever. `ParseCrontab()` not `parseCron()`.
- **Comments:** Only when WHY is non-obvious. No docstrings for obvious functions.
- **Tests:** Each major package gets a `*_test.go` file. Use table-driven tests for edge cases.

---

## Development Workflow

1. **Pick a task** from `TODO.md`
2. **Check out a feature branch:** `git checkout -b task/T1-parser`
3. **Write tests first** (TDD). See existing `*_test.go` for examples.
4. **Implement** the feature. Keep diffs minimal & focused.
5. **Run tests locally:** `go test ./...`
6. **Commit** with message: `feat(parser): validate cron schedules`
7. **Push** and open PR for review.

---

## Common Tasks

### Add a new cron validation rule
1. Edit `cron/parser.go` — add rule to `ValidateSchedule()`
2. Add test case to `cron/parser_test.go`
3. Run `go test ./cron`
4. Commit

### Change an API response format
1. Update struct in `main.go` (e.g., add new field to `Entry`)
2. Update handler (e.g., `handleGetCron()`)
3. Add test in `main_test.go`
4. Update frontend in `ui/index.html` to handle new field
5. Run full test suite: `go test ./...`
6. Commit

### Fix a bug in the UI
1. Edit `ui/index.html` (inline JS)
2. Reload browser at `http://localhost:8080` (recompile binary if embedded file changed)
3. Test manually
4. Commit

---

## Environment Variables

| Var | Default | Purpose |
| --- | --- | --- |
| `HOURGLASS_BIND` | `127.0.0.1:8080` | Server bind address |
| `HOURGLASS_AUTH_USER` | (none) | Basic auth username (optional) |
| `HOURGLASS_AUTH_PASS` | (none) | Basic auth password (optional) |

---

## Deployment

**See `README.md` (post-MVP) for user instructions.**

### Local development
```bash
go run main.go
```

### Build release binary
```bash
# GitHub Actions does this automatically on tag
git tag v0.1.0
git push origin v0.1.0
# Check GitHub Actions for build progress
```

### Manual systemd install (future)
```bash
sudo cp hourglass /usr/local/bin/
sudo systemctl enable hourglass
sudo systemctl start hourglass
```

---

## Troubleshooting

### "Cannot read crontab: permission denied"
- Running as wrong user. Try: `sudo ./hourglass`
- Crontab not initialized. Try: `crontab -e` in system, then reload

### "Address already in use :8080"
- Port 8080 taken. Try: `HOURGLASS_BIND=127.0.0.1:9000 ./hourglass`

### UI not loading
- Recompile binary (changes to `ui/index.html` require rebuild)
- Browser cache. Open DevTools → clear cache → reload

---

## References

- **Architecture & decisions:** See `Design.md`
- **Implementation checklist:** See `TODO.md`
- **User guide (post-MVP):** See `README.md`
- **Go crontab format:** [man crontab(5)](https://man7.org/linux/man-pages/man5/crontab.5.html)
- **Go stdlib:** [net/http](https://pkg.go.dev/net/http), [os/exec](https://pkg.go.dev/os/exec)

---

## Questions?

Refer to `Design.md` for architecture rationale, `TODO.md` for task breakdown, or review git history for context on past decisions.
