# Hourglass Implementation Tasks — MVP

**Status:** Planning phase  
**Start Date:** TBD  
**Target Release:** Single-binary crontab UI with execution history for Linux

**MVP includes:**
- View configured cron jobs
- See when each job last ran
- See exit status (success/failure)
- Add/edit/delete jobs
- No external dependencies
- Automatic syslog parsing

---

## MVP Tasks (Single Phase)

### T1 — Cron schedule parser with validation

**File:** `cron/parser.go` + `cron/parser_test.go`

**What:** Parse crontab text into Entry structs and validate schedules.

**Implementation:**
- `ParseCrontab(text string) ([]Entry, error)` — parse lines to entries
- `ValidateSchedule(schedule string) error` — validate "0 9 * * *" format
- Support: valid ranges, comments (#), blank lines, inactive entries
- Handle edge cases: empty input, malformed schedules

**Effort:** ~2h human / ~20min CC

**Tests:**
- Valid schedules (multiple ranges, wildcards, lists)
- Invalid schedules (out of range, bad names)
- Comments and blank lines
- Round-trip (parse → stringify → parse)

**Definition of Done:**
- All tests pass
- Parser handles all crontab formats
- Clear error messages for invalid input

---

### T2 — Crontab read/write with safety

**File:** `cron/manager.go` + `cron/manager_test.go`

**What:** Interface with Linux crontab via os/exec with error recovery.

**Implementation:**
- `ReadCrontab() (string, error)` — execute `crontab -l`, handle errors
- `WriteCrontab(entries []Entry) error` — execute `crontab -` with stdin
- Read-before-write fallback (safety net)
- Error handling: no crontab, permission denied, daemon issues

**Effort:** ~3h human / ~20min CC

**Tests:**
- Read existing crontab
- Read when no crontab exists
- Read permission denied
- Write valid entries
- Write permission denied
- Read-before-write recovery

**Definition of Done:**
- All tests pass with mocked os/exec
- Safe error handling
- Read-before-write works

---

### T3 — HTTP API endpoints (configuration)

**File:** `main.go` (handlers)

**What:** REST API to read/write cron jobs.

**Endpoints:**
- `GET /` — serve embedded `ui/index.html`
- `GET /api/cron` — return JSON array of Entry (with last execution data)
- `POST /api/cron` — accept entries, validate, write to system

**Implementation:**
- Parse JSON request body
- Validate using T1 (schedule validator)
- Call T2 (manager) to write
- Return structured error responses
- Add CORS headers (if needed for remote access)

**Effort:** ~2h human / ~15min CC

**Tests:**
- GET /api/cron returns list
- POST /api/cron with valid job
- POST /api/cron with invalid schedule (400)
- POST /api/cron with malformed JSON (400)
- Error responses have detail field

**Definition of Done:**
- All endpoints work
- Error messages are user-friendly
- JSON schema is consistent

---

### T4 — Syslog history reader

**File:** `cron/history.go` + `cron/history_test.go`

**What:** Parse journalctl to extract cron execution records.

**Implementation:**
- Execute: `journalctl -u cron -o json --since="24h ago"`
- Parse JSON output for CRON entries
- Extract: timestamp, command, exit code
- Match command to Entry (by comparing job command)
- Cache results with ~30s TTL

**Key function:**
```go
func (m *Manager) GetLastExecution(cmd string) (*Execution, error) {
  // Find most recent execution for this command
  // Return: timestamp, exit code, status (success/failed)
}
```

**Effort:** ~4h human / ~25min CC

**Tests:**
- Parse real journalctl JSON output
- Extract timestamp and exit code
- Match commands correctly (exact match, substring, etc.)
- Handle missing journalctl
- Cache expiration

**Definition of Done:**
- All tests pass
- Correctly matches jobs to executions
- Handles edge cases (no logs, command not found, etc.)

---

### T5 — Enrich Entry struct with execution data

**File:** `cron/manager.go` (extend), `main.go` (API)

**What:** Add last execution info to Entry struct and auto-refresh.

**Implementation:**
- Add fields to Entry: `LastRun *time.Time`, `LastStatus string`, `LastExitCode int`
- Background goroutine that refreshes history every 30s
- GET /api/cron includes these fields in response
- Handle missing data gracefully (new system, no logs)

**Background refresh logic:**
```go
func (m *Manager) startHistoryRefresh() {
  ticker := time.NewTicker(30 * time.Second)
  for range ticker.C {
    // Refresh execution history for all entries
    // Update Entry structs with latest data
  }
}
```

**Effort:** ~2h human / ~15min CC

**Tests:**
- Entry struct has new fields
- GET /api/cron returns fields
- Background refresh works
- History updates are thread-safe

**Definition of Done:**
- Entry includes execution data
- API returns complete information
- Background refresh is reliable

---

### T6 — Single-file frontend UI

**File:** `ui/index.html` (embedded)

**What:** Responsive dashboard showing job status and execution history.

**Table columns:**
1. Schedule (e.g., "0 9 * * *")
2. Command (job name)
3. Comment (user description)
4. Last Run (e.g., "2h 45m ago" or "Never")
5. Status (✓ green for success, ✗ red for failure, - for never run)
6. Actions (Edit, Delete buttons)

**Features:**
- Fetch /api/cron on page load
- Render job table
- Add job form (schedule + command + comment)
- Edit job form (modal)
- Delete confirmation
- Error messages for invalid input
- Highlight failed jobs or jobs that haven't run
- Network error handling ("Reconnecting...")

**Styling:**
- Tailwind CSS via CDN (no build step)
- Responsive (works on mobile)
- Clear status indicators (colors, icons)

**Effort:** ~4h human / ~15min CC

**Tests:**
- E2E: Load page, fetch jobs, display table
- E2E: Add job, POST succeeds, list refreshes
- E2E: Edit job, changes appear
- E2E: Delete job, confirm dialog, removed
- E2E: Invalid schedule shows error

**Definition of Done:**
- UI loads in <1s
- All CRUD operations work
- Status display is accurate and clear
- Mobile-friendly

---

### T7 — Full test suite

**Files:** `*_test.go` in all packages

**What:** Comprehensive unit, integration, and E2E tests.

**Unit tests:**
- Parser: valid/invalid schedules, edge cases
- Manager: read/write crontab (mocked), error handling
- History: journalctl parsing, command matching

**Integration tests:**
- API endpoints: GET/POST, JSON responses
- Manager + Parser: round-trip (write then read)
- History + API: history data in response

**E2E tests:**
- Start server, POST job, verify in list
- Fetch job, edit, verify update
- Delete job, verify removal
- Network error handling

**Mocking:**
- Mock os/exec (crontab calls)
- Mock os/exec (journalctl calls)
- Use httptest for API tests

**Coverage target:** 87% of code paths

**Effort:** ~5h human / ~30min CC

**Definition of Done:**
- All tests pass on Ubuntu 20.04 LTS
- Coverage report shows 87%+
- No flaky tests
- Test output is clear

---

### T8 — GitHub Actions CI/CD

**File:** `.github/workflows/build.yml`

**What:** Automated testing and release builds.

**Pipeline:**
1. On every push: Run tests (all platforms)
2. Build matrix: linux/darwin, amd64/arm64
3. On tag: Build + publish to GitHub Releases

**Build steps:**
- Install Go 1.21+
- Run tests (`go test ./...`)
- Build binaries for all platforms
- Create GitHub Release with binaries
- Generate checksums

**Effort:** ~1h human / ~10min CC

**Definition of Done:**
- CI runs on all pushes
- Tests must pass
- Releases auto-publish on tag v*.*.* 
- Binaries are available for download

---

### T9 — README and deployment guide

**File:** `README.md` (user-facing)

**What:** Complete guide for end users.

**Sections:**
- **Overview:** What is Hourglass?
- **Features:** What it does (config, history, no setup)
- **Requirements:** Linux, crontab, journalctl access
- **Installation:**
  - Download from GitHub Releases
  - Or: `go build -o hourglass .`
- **Usage:**
  - Run locally: `./hourglass` (opens http://localhost:8080)
  - Run on server: Systemd service config
  - Remote access: SSH tunnel instructions
- **Configuration:**
  - Environment variables (BIND, AUTH_USER, AUTH_PASS)
  - Systemd service file
- **Troubleshooting:**
  - "Permission denied" → run as root or add to syslog group
  - "Cannot access journalctl" → check permissions
  - "Binary not found" → download from Releases
- **Limitations:**
  - Single-admin (document concurrent edit risk)
  - Linux only (macOS future)
  - History limited to syslog retention (1-4 weeks)
- **Contributing:** How to report issues, contribute PRs

**Effort:** ~1h human / ~5min CC

**Definition of Done:**
- Clear and complete
- All setup steps work
- Troubleshooting covers common issues
- Users can deploy without external help

---

## Success Criteria for MVP Release

All of these must be true:

- [ ] Binary compiles to <15MB single file
- [ ] Reads any valid system crontab
- [ ] Shows all configured jobs in table
- [ ] **Shows last run time** (e.g., "2h 45m ago") ← from journalctl
- [ ] **Shows exit status** (✓ Success or ✗ Failed)
- [ ] **Shows last exit code** (0, 1, etc.)
- [ ] Add/edit/delete jobs via UI
- [ ] Validates cron schedules before write
- [ ] Graceful error handling for all failure modes
- [ ] Automatically parses journalctl (no setup)
- [ ] Refreshes history every 30s
- [ ] All tests pass (87% coverage)
- [ ] UI loads in <1s
- [ ] Works on Ubuntu 20.04 LTS
- [ ] GitHub Actions CI/CD passes
- [ ] README deployment guide complete

---

## Effort Summary

| Task | Human | CC | Total |
|------|-------|----|----|
| T1 — Parser | 2h | 20min | 2h 20min |
| T2 — Manager | 3h | 20min | 3h 20min |
| T3 — API | 2h | 15min | 2h 15min |
| T4 — History reader | 4h | 25min | 4h 25min |
| T5 — Data enrichment | 2h | 15min | 2h 15min |
| T6 — UI | 4h | 15min | 4h 15min |
| T7 — Tests | 5h | 30min | 5h 30min |
| T8 — CI/CD | 1h | 10min | 1h 10min |
| T9 — Docs | 1h | 5min | 1h 5min |
| **TOTAL** | **24h** | **2h 35min** | **~27h** |

---

## Dependencies & Parallelization

**Sequential chain:**
```
T1 → T2 → T3 → T5 ← T4
            ↓
            T6 → T7 → T8 → T9
```

**Parallelizable:**
- T4 (history reader) can start while T3 runs
- T6 (UI) can start while T5 is wrapping up

**Fastest path (with parallelization):**
- Start T1
- While T1 runs: prep T2, T4, T6 locally
- T1 → T2 → (T3 + T4 in parallel) → T5 → T6 → (T7 + T8 + T9 in parallel)

---

## Progress Tracking

As you complete tasks, mark them done:

- [ ] T1 — Cron schedule parser ← Start here
- [ ] T2 — Crontab read/write
- [ ] T3 — HTTP API
- [ ] T4 — Syslog history reader
- [ ] T5 — Data enrichment
- [ ] T6 — UI
- [ ] T7 — Tests
- [ ] T8 — CI/CD
- [ ] T9 — Docs

**Milestone:** All tasks complete → Ship v1.0 🎉

---

## Notes

### Why single phase?

All 9 tasks are MVP. No feature is "nice-to-have" — execution history is core, not optional.

### Why 24 hours?

Includes parsing, safety, UI, testing, CI/CD, docs. Not just a quick hack.

### Can I parallelize more?

Yes, if you have multiple developers:
- Dev A: T1 + T2 (backend infrastructure)
- Dev B: T4 (history reader)
- Dev C: T6 (UI)
- Dev D: T7 (tests, can start early)

### When can I release?

When all 9 tasks are done AND success criteria met. No shortcuts.
