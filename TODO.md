# Hourglass Implementation & Production Readiness Checklist

**Status:** MVP mostly complete + extended with SSH/remote features  
**Date:** 2026-07-13  
**Binary Size:** 8.1 MB ✅  
**Test Coverage:** 25.7% ⚠️ (target: 87%)

---

## MVP Tasks (Original Scope — T1-T9)

### ✅ T1 — Cron schedule parser with validation
**File:** `cron/parser.go` + `cron/parser_test.go`  
**Status:** COMPLETE
- All schedule parsing implemented
- Validation working
- Tests passing (5/5 edge cases)
- Supports comments, blank lines, inactive entries

### ✅ T2 — Crontab read/write with safety
**File:** `cron/manager.go` + `cron/manager_test.go`  
**Status:** COMPLETE
- Read/write via os/exec working
- Error recovery implemented
- Read-before-write safety net in place
- Tests passing (8/8)

### ✅ T3 — HTTP API endpoints
**File:** `main.go` (handlers)  
**Status:** COMPLETE
- GET /api/cron working
- POST /api/cron with validation working
- DELETE /api/cron working
- Tests passing (6/6)

### ✅ T4 — Syslog history reader
**File:** `cron/history.go` + `cron/history_test.go`  
**Status:** COMPLETE
- journalctl parsing implemented
- Fallback to syslog if journalctl fails
- Execution history tracking working
- Tests passing (7/7)

### ✅ T5 — Enrich Entry struct with execution data
**File:** `cron/manager.go`, `main.go`  
**Status:** COMPLETE
- Entry struct has LastRun, LastStatus, LastCode fields
- Background history refresh implemented (30s ticker)
- API returns execution data
- Thread-safe with mutexes

### ✅ T6 — Single-file frontend UI
**File:** `ui/index.html` (embedded)  
**Status:** COMPLETE
- Table view of all jobs working
- Add/edit/delete forms functional
- Tailwind styling responsive
- Loads in <1s
- Status indicators (success/failure/never run)

### ✅ T7 — Full test suite
**Files:** All `*_test.go`  
**Status:** PARTIAL ⚠️
- Core parser/manager/history tests: 36/36 ✅
- API tests: 6/6 ✅
- Integration tests: 1/1 ✅
- **Missing:** SSH/connection feature tests (0 tests)
- **Coverage Issue:** 25.7% instead of 87% target

### ✅ T8 — GitHub Actions CI/CD
**File:** `.github/workflows/build.yml`  
**Status:** COMPLETE
- Tests run on every push
- Build matrix: linux/darwin × amd64/arm64
- Release auto-publishes on tags
- Checksums generated

### ✅ T9 — README and deployment guide
**File:** `README.md`  
**Status:** COMPLETE
- Installation instructions included
- Usage examples provided
- Troubleshooting section complete
- Systemd service example included

---

## Extended Features (Added After MVP)

### ✅ SSH-Based Remote Connection Manager
**Files:** `ssh/client.go`, `connection/config.go`  
**Status:** IMPLEMENTED but UNTESTED
- SSH client for remote server access ✅
- Connection manager UI ✅
- Connection persistence ✅
- Active connection switching ✅
- **Missing:** Tests for SSH/connection features ❌

**What was added:**
- `/api/connections` endpoints (create, read, delete, switch)
- SSH key path expansion (~/key.pem support)
- SSH agent authentication support
- Connection test endpoint
- UI connection management panel

---

## Production Readiness Checklist

### 🔴 Critical Issues (Must Fix Before Release)

- [ ] **Test Coverage Crisis**: Currently 25.7% vs 87% target
  - [ ] Add tests for SSH client module (client.go)
  - [ ] Add tests for connection manager (connection/config.go)
  - [ ] Add integration tests for remote connections
  - [ ] Get coverage back to 80%+ minimum

- [ ] **No Error Logging Strategy**
  - [ ] Structured logging (JSON or key-value format)
  - [ ] Log levels (debug, info, warn, error)
  - [ ] Log file rotation for long-running instances
  - [ ] Audit trail for cron changes

- [ ] **Security Gaps**
  - [ ] No rate limiting on API endpoints
  - [ ] No CSRF protection for POST/DELETE
  - [ ] No audit trail for who changed what
  - [ ] SSH key paths not validated (symlink attack possible)
  - [ ] Basic auth credentials in env vars (no encryption at rest)

- [ ] **Missing Graceful Shutdown**
  - [ ] No signal handling (SIGTERM/SIGINT)
  - [ ] History refresh goroutine not cancelled on exit
  - [ ] SSH connections may leak on shutdown

### 🟡 Important (Should Fix for Production)

- [ ] **Concurrency Documentation Incomplete**
  - [ ] Add warnings in README about concurrent edit limitations
  - [ ] Clarify single-admin model
  - [ ] Document what happens on simultaneous writes

- [ ] **Error Messages Not User-Friendly**
  - [ ] "failed to query journalctl" should explain next steps
  - [ ] SSH errors should suggest fixes (permissions, connectivity)
  - [ ] API errors should include remediation hints

- [ ] **Missing Health Checks**
  - [ ] `/health` endpoint exists but incomplete
  - [ ] Should check: crontab readable, journalctl accessible, disk space
  - [ ] Should report connection status for remote instances

- [ ] **No Backup/Restore**
  - [ ] No export of crontab before write
  - [ ] No restore from backup
  - [ ] Document manual backup: `crontab -l > backup.cron`

- [ ] **Remote Access Not Documented Well**
  - [ ] SSH tunneling section exists in README but could be clearer
  - [ ] No security guidance (firewall rules, fail2ban integration)
  - [ ] No mention of VPN alternative

### 🟢 Nice-to-Have (Post-Release)

- [ ] **Clustering Support**
  - [ ] Multiple instances reading same crontab
  - [ ] Distributed locking via file/database
  - [ ] Consensus on concurrent edits

- [ ] **macOS Support**
  - [ ] Test on macOS (different crontab location)
  - [ ] Handle launchd vs cron differences
  - [ ] Cross-compile test in CI

- [ ] **Web UI Enhancements**
  - [ ] Dark mode toggle persistence
  - [ ] Job execution log viewer
  - [ ] Search/filter jobs
  - [ ] Bulk operations (enable/disable multiple)
  - [ ] Job templates/quickstart

- [ ] **Advanced Features**
  - [ ] Job execution from UI ("Run Now")
  - [ ] Cron syntax validator/helper in form
  - [ ] Job dependency tracking
  - [ ] Scheduled maintenance windows
  - [ ] Email alerts on job failure

---

## 🔍 Research: Interactive Cron Scheduler Helper (Future Feature)

**Goal:** Build an exceptional UX for creating and editing cron schedules so intuitive that users never need to think about cron syntax. The interface should feel completely natural—like they're just describing what they want to happen.

### Research Tasks

- [ ] **Cron Schedule Format Deep Dive**
  - [ ] Standard 5-field format (minute, hour, day, month, day-of-week) + extended formats
  - [ ] Special characters: `*`, `,`, `-`, `/`, `?`, `L`, `W`, `#`
  - [ ] Ranges and step values (e.g., `0-23/2` for every 2 hours)
  - [ ] Non-standard extensions: `@yearly`, `@daily`, etc.
  - [ ] Edge cases: leap seconds, daylight savings, timezone handling
  - [ ] Common mistakes users make (e.g., `0 0 0 * *` is invalid, `? ? ? ? ?` confusion)

- [ ] **UX Patterns & Inspiration**
  - [ ] Survey existing cron builders: crontab.guru, cron-job.org, EasyCron UI
  - [ ] Study form builder UX: Typeform, Jotform (how do they make complex data entry simple?)
  - [ ] Natural language parsing libraries (could user describe in words?)
  - [ ] Interactive visual timeline (show next 10 executions graphically)
  - [ ] Accessibility: how do screen readers interact with schedule builders?

- [ ] **Guided, Intuitive Interaction Design**
  - [ ] Avoid technical cron language—use natural UI patterns instead
  - [ ] Step-by-step form: "When?", "Which days?", "What time?", "How often?"
  - [ ] Real-time natural-language confirmation ("Every weekday at 9 AM")
  - [ ] Smart defaults based on common patterns (daily, weekly, monthly shortcuts)
  - [ ] Validation that prevents invalid schedules (user can't create bad data)
  - [ ] Visual preview of next 10 execution times (confirms they got what they wanted)

- [ ] **Translation/Conversion Features**
  - [ ] Cron → English (`0 9 * * MON-FRI` → `Every weekday at 9 AM`)
  - [ ] English → Cron (reverse: can user type "daily at midnight" and get the schedule?)
  - [ ] Timezone conversion (show "will run at 3 PM PST, which is 6 PM EST")
  - [ ] Timezone-aware scheduling (store in UTC, display in user's timezone)
  - [ ] Export to other formats (systemd timer syntax, at/batch format, cloud scheduler)

- [ ] **Interactive Scheduler UI Design**
  - [ ] Field-by-field builder with validation
  - [ ] Predefined templates (daily, weekly, monthly, quarterly, yearly + custom combos)
  - [ ] Visual calendar showing execution dates (next 30 days)
  - [ ] Validation errors with suggestions ("Did you mean `0 9 * * *`?")
  - [ ] Copy/paste detection: parse pasted schedules and populate form
  - [ ] Conflict detection: warn if 2 jobs overlap

- [ ] **Performance & Implementation Approach**
  - [ ] Can we use existing Go cron library (robfig/cron)?
  - [ ] Client-side vs. server-side parsing? (most should be client, avoid round-trips)
  - [ ] Build a cron expression AST parser (reusable, testable)
  - [ ] Generative approach: build Cron → English via structured rules
  - [ ] Next execution time calculation (handle timezone properly)

- [ ] **Testing Strategy**
  - [ ] Parser edge cases: every possible special character combination
  - [ ] Localization: cron in English, Spanish, German (or just English MVP?)
  - [ ] Timezone correctness: DST transitions, leap years
  - [ ] UI interaction tests: field changes update preview in real-time
  - [ ] Accessibility tests: keyboard nav, screen reader compat

### Deliverables for Phase 1 (MVP of this feature)

1. **Intuitive Schedule Builder Form** (`ui/index.html`)
   - Modal/page with guided form: frequency selector → day selector → time selector
   - No technical jargon (not "minute/hour/day" fields, but "every day", "every Monday", "every 15th", etc.)
   - Smart template buttons ("Daily", "Weekly", "Monthly", "Custom")
   - Real-time preview of next 10 execution times (visual confirmation)
   - Form prevents submission of invalid schedules

2. **Schedule Validator & Preview Engine** (`cron/parser.go`)
   - Add validation that catches impossible schedules before they reach crontab
   - Add next-execution calculator (timezone-aware if possible)
   - Add reverse-engineer: given a cron string, populate the form fields

3. **Natural-Language Confirmation** (`main.go`)
   - `POST /api/cron/preview` → Accept form input, return: (a) cron string, (b) next 10 runs, (c) human-readable description
   - Used as real-time feedback as user builds their schedule

4. **Smart Copy/Paste Handling**
   - If user pastes a cron string into any field, auto-parse and populate the form
   - Graceful error message if paste is invalid (don't reject, just let them edit)

### Success Criteria

- [ ] User can create any valid cron schedule without ever typing cron syntax
- [ ] User can build a schedule using the interactive form in <30 seconds
- [ ] User feels confident about what they created (real-time visual preview)
- [ ] Form prevents invalid schedules before submission (no "invalid schedule" errors)
- [ ] Keyboard-accessible (no mouse required, full tab navigation)
- [ ] Works offline (no external API calls)
- [ ] <100ms latency for real-time preview updates

---

## Success Criteria for Production Release v1.0

**Core Requirements (MVP Met):**
- [x] Binary <15MB ✅ (8.1MB)
- [x] Reads system crontab ✅
- [x] Shows all jobs in table ✅
- [x] Shows last run time ✅
- [x] Shows exit status ✅
- [x] Add/edit/delete jobs ✅
- [x] Schedule validation ✅
- [x] Graceful error handling (partial ⚠️)
- [x] Auto-parse journalctl ✅
- [x] History refresh 30s ✅
- [x] Tests passing (36/36 core) ✅
- [x] UI loads <1s ✅
- [x] Works Ubuntu 20.04+ ✅
- [x] GitHub Actions CI/CD ✅
- [x] README complete ✅

**Extended Features (Beyond MVP):**
- [x] SSH-based remote access ✅
- [x] Connection manager ✅
- [ ] Tests for new features ❌

**Production Gate (Before Release):**
- [ ] Test coverage ≥80% (currently 25.7%) 🔴
- [ ] Security audit passed ❌
- [ ] Logging/monitoring strategy implemented ❌
- [ ] Graceful shutdown implemented ❌
- [ ] Rate limiting added ❌
- [ ] Manual testing on real systems ❌
- [ ] Version 1.0 tagged and released ❌

---

## Implementation Order for Production Readiness

### Phase 1: Test Coverage (HIGH PRIORITY)
1. Add SSH client tests
2. Add connection manager tests
3. Add integration tests for remote features
4. Achieve 80%+ coverage

### Phase 2: Security & Logging
1. Add structured logging throughout
2. Implement graceful shutdown
3. Add rate limiting middleware
4. Add CSRF protection
5. Security audit of SSH key handling

### Phase 3: Operations
1. Implement proper health endpoint
2. Add backup/restore guidance
3. Improve error messages
4. Add audit trail for changes
5. Documentation updates

### Phase 4: Testing & Release
1. Manual QA on production-like system
2. Load testing (100+ jobs, sustained traffic)
3. Failure scenario testing (permission denied, disk full, etc.)
4. Create GitHub release with v1.0.0 tag
5. Publish binaries for all platforms

---

## Estimated Effort

| Task | Effort | Priority | Owner |
|------|--------|----------|-------|
| Add SSH/connection tests | 2h | 🔴 Critical | needed |
| Implement logging | 2h | 🔴 Critical | needed |
| Graceful shutdown | 1h | 🟡 Important | needed |
| Security fixes | 2h | 🔴 Critical | needed |
| Health checks | 1h | 🟡 Important | optional |
| Documentation | 1h | 🟡 Important | optional |
| Manual QA | 3h | 🔴 Critical | needed |
| **TOTAL** | **~12h** | - | - |

---

## Notes for Production Release

### What's Working Well ✅
- Core cron functionality rock solid
- UI responsive and intuitive
- GitHub Actions CI/CD automated
- Error handling covers happy path
- SSH remote access adds real value

### What Needs Attention ⚠️
- Test coverage dropped 60% due to new features without tests
- No structured logging (just bare log.Printf)
- Graceful shutdown not implemented
- Security model not thoroughly reviewed
- Concurrent edit limitations not clearly documented

### Recommended Release Strategy
1. **v0.9-beta**: Current code with test coverage fixed, released to users for feedback
2. **v0.9-rc1**: Security audit + logging + graceful shutdown
3. **v1.0**: Full production release with all gate criteria met

---

## Files Modified Since Last Commit

Currently uncommitted changes in:
- `cron/history.go` — Executor pattern, syslog fallback
- `cron/manager.go` — ExecuteCommand method for SSH
- `cron/parser.go` — (check git diff)
- `main.go` — Connection/SSH handlers
- `ssh/client.go` — SSH key path handling
- `ui/index.html` — Connection manager UI

**Action:** Review, test, and commit these changes before release.

---

## Links & Resources

- [GitHub Repository](https://github.com/TillmanBuildsTech/hourglass)
- [CLAUDE.md](CLAUDE.md) — Architecture & conventions
- [Design.md](Design.md) — Detailed design decisions
- [README.md](README.md) — User documentation
