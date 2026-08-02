# Hourglass Design Document

**Project:** Self-hosted Crontab UI  
**Runtime:** Go 1.21+  
**Target:** Linux (primary), macOS (future)  
**Status:** Architecture Review CLEARED (2026-07-09)

---

## 1. Problem Statement

System administrators need a lightweight, web-based interface to view and manage Linux crontab jobs. The interface must show:
- **What's configured:** All scheduled jobs (schedule + command)
- **What happened:** When each job last executed and its status
- **How to manage:** Add/edit/delete jobs without SSH access

The tool must:
- Run as a single, self-contained binary (<15MB)
- Require no external dependencies (Go stdlib only)
- Parse system logs automatically (no user setup)
- Support remote access over HTTPS/SSH tunnel
- Gracefully handle permission issues and edge cases

---

## 2. Architecture Overview

### System Diagram

```
┌──────────────────────┐
│   Browser UI         │
│  (index.html)        │
│                      │
│ Shows:               │
│ • Job schedule       │
│ • Last run time      │
│ • Exit status ✓/✗    │
└──────────┬───────────┘
           │ HTTP (GET /api/cron, POST /api/cron)
           ▼
┌──────────────────────────────────────┐
│    Go HTTP Server :8080              │
│                                      │
│  ┌─ Crontab Manager ─────────────┐  │
│  │ • Read/write crontab config   │  │
│  │ • Validate schedules          │  │
│  │ • Error handling              │  │
│  └──────────────────────────────┘  │
│                                      │
│  ┌─ History Reader (syslog) ─────┐  │
│  │ • Parse journalctl -u cron     │  │
│  │ • Extract: timestamp, exit code│  │
│  │ • Cache results (30s refresh)  │  │
│  │ • Match to cron entries        │  │
│  └──────────────────────────────┘  │
│                                      │
│  ┌─ HTTP Handlers ────────────────┐  │
│  │ • GET /api/cron (config+last)  │  │
│  │ • GET /api/cron/{id}/history   │  │
│  │ • POST /api/cron (write)       │  │
│  │ • GET / (serve UI)             │  │
│  └──────────────────────────────┘  │
└──────────┬──────────────────────────┘
           │ os/exec (crontab CLI)
           ├──────────────────────────┐
           │                          │
           ▼                          ▼
┌──────────────────────┐   ┌─────────────────────┐
│ System Crontab       │   │ Syslog / journalctl │
│ /var/spool/cron/*    │   │ (read-only)         │
│                      │   │                     │
│ • Job configs        │   │ • Execution logs    │
│ • Schedules          │   │ • Exit codes        │
│ • Commands           │   │ • Timestamps        │
└──────────────────────┘   └─────────────────────┘
```

### Data Structures

```go
type Entry struct {
  ID          string      // UUID for tracking
  Schedule    string      // "0 9 * * *" (5 fields)
  Command     string      // Full shell command
  Comment     string      // User-facing description
  Active      bool        // true if uncommented
  LastRun     *time.Time  // When job last executed (from syslog)
  LastStatus  string      // "success" | "failed" | "unknown"
  LastExitCode int        // Exit code from last execution
  LastDuration int        // Seconds (optional, from syslog if available)
}

type Execution struct {
  ID        string    // Matches Entry.ID
  Timestamp time.Time // When job started
  ExitCode  int       // 0 = success, non-zero = failure
  Duration  int       // Seconds
}
```

### Core Packages

| Package | Purpose |
| --- | --- |
| `main.go` | HTTP server, routing, embedded FS |
| `cron/parser.go` | Parse crontab text → Entry structs, validate schedules |
| `cron/manager.go` | Read/write crontab via os/exec, error handling |
| `cron/history.go` | **NEW:** Parse syslog (journalctl), extract execution history |
| `ui/index.html` | Single-file frontend (Tailwind CDN + vanilla JS) |

---

## 3. Key Design Decisions

### D1: Distribution & CI/CD

**Decision:** Include GitHub Actions in MVP  
**Why:** Users need pre-built binaries, not build instructions  
**Trade-off:** ~1h setup now vs. friction on every release  
**Implementation:** `.github/workflows/build.yml` with cross-platform matrix (linux/darwin, amd64/arm64)

---

### D2: Platform Scope

**Decision:** Linux-only MVP; macOS deferred  
**Why:** Minimal scope, clear target. macOS uses `launchd`, not crontab.  
**Trade-off:** Users can extend architecture later without rework  
**Implementation:** Document Linux requirement clearly. Post-launch, add OS detection.

---

### D3: Concurrency Model

**Decision:** No locking. First-write-wins.  
**Why:** Simplest for MVP. Assumes single admin.  
**Trade-off:** Data loss if two edits happen simultaneously  
**Implementation:** Document as single-admin tool. Add roadmap item for pessimistic locking post-launch.  
**Risk:** Silent data loss. Mitigate by warning users in README.

---

### D4: Permission Model

**Decision:** Run as current user only. No sudo/root.  
**Why:** Safe, auditable, aligns with typical sysadmin use.  
**Trade-off:** Can't edit other users' crontabs. Workaround: deploy multiple instances.  
**Implementation:** No special escalation logic in code.

---

### D5: Error Handling

**Decision:** User-friendly API error messages  
**Why:** Prevents cryptic system errors reaching users  
**Trade-off:** Requires robust validation before system calls  
**Implementation:**
- Schedule validation before `crontab -` call
- Catch stderr from system, parse common errors
- Return structured JSON: `{error: "Invalid minute: 99, must be 0-59"}`

---

### D6: Write Safety

**Decision:** Read-before-write with fallback  
**Why:** Protects against data loss if write partially fails  
**Trade-off:** Extra system call (negligible performance cost)  
**Implementation:**
- Before `POST /api/cron`, read current crontab
- If write fails, log that original crontab is still valid
- User can retry or investigate system issues

---

### D7: API Design

**Decision:** Start with `/api/cron`, defer versioning  
**Why:** MVP doesn't need versioning yet. Simple to refactor to `/v1/` later.  
**Trade-off:** Slight extra effort when versioning becomes necessary  
**Implementation:** Use `/api/cron` now. When features expand (history, webhooks), migrate to `/v1/cron`.

---

### D8: Frontend

**Decision:** Single-file HTML with Tailwind CDN  
**Why:** Zero build step, no Node.js required, minimal footprint  
**Trade-off:** Runtime CSS loading (negligible, Tailwind is cached by CDN)  
**Implementation:** Vanilla JS (fetch API), embed in binary via `//go:embed`.

---

## 4. Critical Paths & Failure Scenarios

### Read Flow: GET /api/cron

```
1. User loads browser UI
2. JS calls: GET /api/cron
3. Go handler calls: cron.ReadCrontab()
4. crontab -l executed
   ├─ Success: parse output → JSON response
   ├─ No crontab exists: return [] (empty, not error)
   └─ Permission denied: return 500 + "Cannot read crontab: permission denied"
5. Browser displays table or error message
```

**Failure modes & mitigations:**
| Failure | Cause | Mitigation |
| --- | --- | --- |
| `crontab -l` fails | Permission denied, crontab daemon down | Catch stderr, return user-friendly 500 |
| Parse error | Unexpected crontab format | Log error, return 500 + "Invalid crontab format" |
| Network error | Frontend unreachable | Browser handles fetch error, show retry button |

---

### Write Flow: POST /api/cron

```
1. User submits new cron job via form
2. Browser validates locally, sends POST /api/cron
3. Go handler:
   a. Read current crontab (safety baseline)
   b. Parse JSON body → Entry structs
   c. Validate each schedule (e.g., minute 0-59)
   d. Generate crontab text
   e. Execute: crontab - (stdin)
      ├─ Success: return 200 + {success: true}
      └─ Failure: return 400/500 + {error: "..."}
4. Browser refreshes job list on success
```

**Failure modes & mitigations:**
| Failure | Cause | Mitigation |
| --- | --- | --- |
| Invalid schedule | User input "99 * * * *" | Validate before system call, return 400 + message |
| Write permission denied | User permissions issue | Return 500 + "Cannot write crontab" |
| Crontab daemon down | System issue | Return 500 + "Crontab service unavailable" |
| Disk full | System resource issue | Return 500 + "Cannot save (disk full?)" |
| Concurrent write | Two edits simultaneously | First write wins, second gets "crontab modified" (future: add locking) |

---

## 5. Test Strategy

**Target:** 87% code path coverage, 80% user flow coverage

### Unit Tests

| Component | Test File | Coverage |
| --- | --- | --- |
| Schedule parser | `cron/parser_test.go` | Valid/invalid schedules, edge cases |
| Crontab manager | `cron/manager_test.go` | Read/write, error handling (mocked) |
| API handlers | `main_test.go` | GET/POST endpoints, JSON parsing |

### Integration Tests

| Scenario | Approach |
| --- | --- |
| End-to-end user flow | Start server, POST job, verify in list |
| System integration | Mock os/exec; simulate crontab behavior |
| Error paths | Inject failures (bad input, system errors) |

### E2E Tests (Browser)

| User Flow | Tests |
| --- | --- |
| Load page → view jobs | Fetch succeeds, table renders |
| Add job → list updates | Form validation, POST succeeds, refresh works |
| Edit job → schedule changes | Click edit, form populates, submit updates |
| Delete job → disappears | Confirm, DELETE succeeds, refresh works |
| Invalid input → error displays | Submit "99 * * * *", error message shown |

---

## 6. Performance & Scalability

### Assumptions

- Single admin user editing at a time
- <100 cron jobs per user (typical)
- Crontab read/write takes <1s
- Network latency 0-500ms

### Performance Targets

| Metric | Target |
| --- | --- |
| Binary size | <15MB |
| Memory at rest | <10MB |
| API response time | <100ms (local), <500ms (remote) |
| UI load time | <1s |
| Page re-render | <500ms after POST |

### Scalability

**Horizontal:** Not applicable (single-user tool)  
**Vertical:** Can handle 1000+ jobs (parse time O(n), acceptable)  
**Caching:** Browser caches Tailwind CSS from CDN (standard)

---

## 7. Security Considerations

### Authentication

- **Local mode** (`127.0.0.1:8080`): No auth needed (SSH tunnel)
- **Remote mode** (`0.0.0.0:8080`): Optional basic auth via env vars (not production-grade)
- **Future:** Support JWT or OAuth2 for enterprise

### Authorization

- Runs as current user → edits only that user's crontab
- No role-based access control (not in MVP)
- SSH tunnel handles encryption; local mode assumes trusted network

### Data Security

- Crontab in-memory only during read/write (no logging of secrets)
- All jobs sent over HTTPS (when deployed behind reverse proxy)
- No persistent storage beyond system crontab

### Audit

- Log all POST requests (optional feature for future)
- Include timestamps, user (if auth added), operation

---

## 8. MVP Success Criteria

All of these must pass for v1 to ship:

- [ ] Binary compiles to <15MB single file
- [ ] Can read any valid system crontab
- [ ] Can add/edit/delete jobs via API + UI
- [ ] **Shows last run time for each job** ← from syslog
- [ ] **Shows exit status (✓ or ✗)** ← from syslog
- [ ] **Shows last exit code** ← from syslog
- [ ] Graceful error handling for permission issues
- [ ] All tests pass on Ubuntu 20.04 LTS
- [ ] UI loads in <1s
- [ ] Executes `journalctl -u cron` automatically (no user setup)
- [ ] Refreshes execution history every 30s

---

## 10. Known Limitations

| Limitation | Reason | Mitigation |
| --- | --- | --- |
| **Single-admin only** | No concurrency control | Document as single-user tool; add locking post-launch |
| **Linux only** | macOS uses launchd | macOS support deferred to v2 |
| **No user switching** | Complexity + security risk | Run as that user if needed |
| **Syslog retention** | OS config (1-4 weeks typical) | User can extend log retention |
| **No persistent history** | MVP scope | Add SQLite post-launch if needed |
| **No audit trail** | MVP scope | Add who/when/what logging post-launch |

---

## 11. Future Roadmap

**v2 (next release):**

1. **Extended execution history** — Full history stored in SQLite
   - Persistent storage (survives system reboot)
   - Trend analysis (success rate, avg duration)
   - Failure alerts (repeated failures)
   - Export history (CSV/JSON)
   - Effort: ~8h

2. **macOS support** — Detect OS, use launchd on macOS
   - Effort: ~4h

3. **Concurrent edits** — Add pessimistic locking with mutex
   - Effort: ~2h

4. **Audit logging** — Log all modifications (who, when, what)
   - Requires auth layer first
   - Effort: ~4h

5. **Webhooks** — Notify external systems on job add/delete/fail
   - Effort: ~6h

6. **API versioning** — Migrate to `/v1/`, support multiple versions
   - Effort: ~2h

7. **Dark mode toggle** — Enhanced UI
   - Effort: ~1h

8. **Crontab diff** — Show before/after on write
   - Effort: ~2h

9. **Scheduled backups** — Backup crontab to GitHub/S3
   - Effort: ~4h

10. **Mobile app** — Native iOS/Android
    - Effort: ~20h

---

## 12. Reference Architecture

### Build & Deploy

```bash
# Local build
go build -o crontab-ui .

# Docker (future)
docker build -t hourglass .
docker run -p 8080:8080 hourglass

# systemd service (future)
[Unit]
Description=Crontab UI
After=network.target

[Service]
ExecStart=/usr/local/bin/hourglass
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

### Configuration

```bash
# Local-only (default)
./hourglass

# Remote with auth
HOURGLASS_BIND=0.0.0.0:8080 \
HOURGLASS_AUTH_USER=admin \
HOURGLASS_AUTH_PASS=secretpass \
./hourglass
```

---

## 13. Review Sign-Off

**Engineering Review:** 2026-07-09 (Updated 2026-07-09 for execution history scope)  
**Reviewer:** gstack /plan-eng-review  
**Status:** ✅ CLEARED

**Decisions locked:**
- **Scope:** Include CI/CD + execution history in MVP
- **Execution tracking:** Parse syslog automatically (journalctl -u cron)
- **Display:** Last run time, exit code, exit status (✓/✗) per job
- **Refresh:** Every 30s (background)
- **Concurrency:** No locking (single-admin assumption)
- **Errors:** User-friendly messages
- **Safety:** Read-before-write fallback
- **Tests:** 87% coverage target

**Next steps:** See `TODO.md` for implementation tasks.

---

## Appendix A: Cron Execution Tracking Options

> **Update (2026-07-31):** Option 1 (syslog/journalctl parsing) as originally implemented turned out to be non-functional in production — cron never reports exit codes to syslog at all (see [issue #1](https://github.com/TillmanBuildsTech/hourglass/issues/1)), so `LastStatus`/`LastCode` were always empty. Hourglass now uses a variant of Option 2, but automatically: `WriteCrontab` transparently wraps each managed job's command so it logs its own exit code and timestamp to `~/.hourglass/history.log`, and unwraps it back to the original command whenever it's read/edited. No user-facing wrapper script or crontab edits are required — this preserves the "no edits needed" promise while actually working. See `cron/manager.go` (`wrapCommandForHistory`/`unwrapEntry`) and `cron/history.go`.

### Context
Hourglass MVP must show last execution time + status for each job. This is now a core v1 feature.

The Linux cron daemon logs all executions to syslog automatically. We parse these logs in the background (every 30s) and enrich the job list with:
- Last run timestamp
- Exit code
- Status (success ✓ or failure ✗)

### Option 1: System Syslog Parsing (MVP IMPLEMENTATION)

**How it works:**
```bash
# Linux cron daemon logs to syslog automatically
journalctl -u cron --since="24h ago" -o json | grep CRON
# Output: {"message":"(root) CMD (backup.sh)","MESSAGE":"(root) exit status 0"}
```

**Pros:**
- Zero user setup required
- Built-in to Linux
- Captures all job invocations system-wide
- Structured (JSON export available)

**Cons:**
- Requires regex parsing (CRON message format inconsistent)
- Limited details (no stdout capture)
- Log retention depends on syslog config (typically 1-4 weeks)

**Implementation for Hourglass (v2):**
```go
// cron/history.go
type Execution struct {
  ID        string    // matches Entry.ID
  Timestamp time.Time // when job started
  ExitCode  int       // 0 = success
  Duration  int       // seconds
  Message   string    // from syslog
}

func (m *Manager) ReadExecutionHistory(entryID string, limit int) ([]Execution, error) {
  // 1. Execute: journalctl -u cron -o json --since="7 days ago"
  // 2. Parse CRON entries, match to entry by command name
  // 3. Return last N executions
}
```

**Effort:** ~4-5 hours
- Parse journalctl JSON: ~1.5h
- Match to Entry by command: ~1h
- Cache + refresh logic: ~1.5h
- UI display: ~1h

---

### Option 2: User Wrapper Script (Alternative)

**How it works:**
```bash
# User creates wrapper for each job
#!/bin/bash
{
  echo "START $(date -Iseconds)"
  backup.sh 2>&1
  EXIT=$?
  echo "END $(date -Iseconds) EXIT=$EXIT"
} >> /var/log/cron-jobs/backup.log 2>&1

# In crontab:
0 9 * * * /usr/local/bin/cron-wrapper.sh backup.sh
```

**Pros:**
- Full control (can capture stdout, stderr)
- Structured logs (per-job files)
- Works on any Linux distro

**Cons:**
- Requires user to modify every cron entry
- Extra I/O overhead per job
- Log rotation burden on user
- Not transparent

**Not recommended for Hourglass** (breaks our promise of "no edits needed")

---

### Option 3: Timestamp Polling (Lightweight)

**How it works:**
```bash
# User adds sentinel file at end of job
0 9 * * * backup.sh && touch /tmp/backup-done

# Hourglass checks mtime
Execution:
  LastRun: time.Unix(stat.ModTime())
  Status: "Success" if file exists, "Unknown" if missing
```

**Pros:**
- Zero setup (optional)
- Lightweight

**Cons:**
- Unreliable (no execution if job fails)
- No history (only current status)
- No exit code tracking

**Use for:** Quick status indicator only

---

### Recommendation: Hybrid Approach for v2

**Best compromise:**

1. **Default (syslog parsing):** Automatic, zero setup
   - Show: last run time, exit code, duration
   - Data source: journalctl
   - Refresh: every 30s (background)
   - History depth: last 100 executions per job

2. **Optional (wrapper):** Advanced users who want detailed output
   - User can optionally wrap job for stdout capture
   - Hourglass reads `/var/log/cron-jobs/{job-id}.log`
   - Shows: full output, not just exit code

3. **Optional (sentinel):** Lightweight timestamp tracking
   - User adds `&& touch /tmp/{job-id}.done` to job
   - Hourglass uses for quick "is running" check

**API design (v2):**
```go
// GET /api/cron/{id}/history?limit=50&since=7days
{
  "job_id": "backup-1",
  "executions": [
    {
      "timestamp": "2026-07-09T09:00:05Z",
      "exit_code": 0,
      "duration_seconds": 45,
      "status": "success"
    },
    {
      "timestamp": "2026-07-08T09:00:03Z",
      "exit_code": 1,
      "duration_seconds": 2,
      "status": "failed"
    }
  ]
}

// GET /api/cron/{id}/last-run
{
  "job_id": "backup-1",
  "last_run": "2026-07-09T09:00:05Z",
  "exit_code": 0,
  "duration_seconds": 45,
  "status": "success"
}
```

**Timeline:**
- MVP (v1): No execution tracking
- v2 (1 month after MVP): Syslog parsing + UI
- v3 (future): SQLite persistence + trends + alerts

---

### Caveats

1. **journalctl availability:** Not all Linux distros use journalctl (some use syslog only)
   - Solution: Support both `journalctl` and `/var/log/syslog` parsing

2. **Log retention:** Cron logs rotate (typically every 1-4 weeks)
   - Solution: Suggest admins keep logs longer if they need history
   - Can persist history to SQLite post-launch

3. **Performance:** journalctl scan for large time windows is slow
   - Solution: Cache results, refresh incrementally (last 24h)

4. **Permissions:** User running Hourglass needs read access to syslog
   - Solution: Document in README: "Run as root or add user to syslog group"
