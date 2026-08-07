package cron

import (
	"strings"
	"testing"
)

func TestParseValidSchedules(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0 9 * * *", "0 9 * * *"},
		{"*/5 * * * *", "*/5 * * * *"},
		{"0 0 1 1 *", "0 0 1 1 *"},
		{"0-30 8-17 * * 1-5", "0-30 8-17 * * 1-5"},
		{"0,30 * * * *", "0,30 * * * *"},
	}

	for _, tt := range tests {
		if err := ValidateSchedule(tt.input); err != nil {
			t.Errorf("ValidateSchedule(%q) = %v, want nil", tt.input, err)
		}
	}
}

func TestParseInvalidSchedules(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"0 9 * * ", true},
		{"0 9 * * * extra", true},
		{"60 0 * * *", true},
		{"0 24 * * *", true},
		{"0 0 0 * *", true},
		{"0 0 32 * *", true},
		{"0 0 * 0 *", true},
		{"0 0 * 13 *", true},
		{"0 0 * * 8", true},
		{"invalid", true},
	}

	for _, tt := range tests {
		err := ValidateSchedule(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateSchedule(%q) = %v, want error=%v", tt.input, err, tt.wantErr)
		}
	}
}

func TestParseCrontab(t *testing.T) {
	input := `# This is a comment
0 9 * * * /usr/bin/backup.sh
*/5 * * * * /usr/bin/check-disk.sh

30 2 * * 0 /usr/bin/weekly-report.sh
`

	entries, err := ParseCrontab(input)
	if err != nil {
		t.Fatalf("ParseCrontab failed: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(entries))
	}

	tests := []struct {
		idx      int
		schedule string
		command  string
	}{
		{0, "0 9 * * *", "/usr/bin/backup.sh"},
		{1, "*/5 * * * *", "/usr/bin/check-disk.sh"},
		{2, "30 2 * * 0", "/usr/bin/weekly-report.sh"},
	}

	for _, tt := range tests {
		if entries[tt.idx].Schedule != tt.schedule {
			t.Errorf("Entry %d schedule: got %q, want %q", tt.idx, entries[tt.idx].Schedule, tt.schedule)
		}
		if entries[tt.idx].Command != tt.command {
			t.Errorf("Entry %d command: got %q, want %q", tt.idx, entries[tt.idx].Command, tt.command)
		}
	}
}

func TestStringifyCrontab(t *testing.T) {
	entries := []Entry{
		{Schedule: "0 9 * * *", Command: "/usr/bin/backup.sh"},
		{Schedule: "*/5 * * * *", Command: "/usr/bin/check-disk.sh"},
	}

	output := StringifyCrontab(entries)
	expected := "0 9 * * * /usr/bin/backup.sh\n*/5 * * * * /usr/bin/check-disk.sh"

	if output != expected {
		t.Errorf("StringifyCrontab output:\ngot:\n%s\nwant:\n%s", output, expected)
	}
}

// TestStringifyCrontabEscapesPercent guards against a real bug: crontab(5)
// treats a bare '%' in the command field as a newline, splitting the command
// there and feeding the remainder to the job's stdin instead of running it.
// wrapCommandForHistory's printf/date literals contain bare '%' characters,
// which silently truncated every history-tracking write before this fix.
func TestStringifyCrontabEscapesPercent(t *testing.T) {
	entries := []Entry{
		{Schedule: "* * * * *", Command: `printf '%s\n' hi`, Comment: "50% done"},
	}

	output := StringifyCrontab(entries)
	expected := `* * * * * printf '\%s\n' hi # 50\% done`

	if output != expected {
		t.Errorf("StringifyCrontab output:\ngot:\n%s\nwant:\n%s", output, expected)
	}

	// A '%' already escaped by the caller must not be double-escaped.
	already := StringifyCrontab([]Entry{{Schedule: "* * * * *", Command: `echo \%s`}})
	if already != `* * * * * echo \%s` {
		t.Errorf("already-escaped %% was mangled: %s", already)
	}
}

func TestRoundTrip(t *testing.T) {
	original := "0 9 * * * /usr/bin/backup.sh\n*/5 * * * * /usr/bin/check-disk.sh"

	entries, err := ParseCrontab(original)
	if err != nil {
		t.Fatalf("ParseCrontab failed: %v", err)
	}

	output := StringifyCrontab(entries)

	if output != original {
		t.Errorf("Round-trip failed:\noriginal:\n%s\noutput:\n%s", original, output)
	}
}

func TestInactiveEntryPreservesComment(t *testing.T) {
	// Regression: toggling a job to inactive must not drop its comment/name.
	entry := Entry{
		Schedule: "* * * * *",
		Command:  "/usr/bin/backup.sh",
		Comment:  "Daily Backup",
		Inactive: true,
	}

	out := StringifyCrontab([]Entry{entry})
	want := "# * * * * * /usr/bin/backup.sh # Daily Backup"
	if out != want {
		t.Fatalf("StringifyCrontab inactive = %q, want %q", out, want)
	}

	parsed, err := ParseCrontab(out)
	if err != nil {
		t.Fatalf("ParseCrontab failed: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(parsed))
	}
	p := parsed[0]
	if !p.Inactive {
		t.Error("expected parsed entry to be inactive")
	}
	if p.Comment != "Daily Backup" {
		t.Errorf("Comment = %q, want %q", p.Comment, "Daily Backup")
	}
	if p.Schedule != "* * * * *" {
		t.Errorf("Schedule = %q, want %q", p.Schedule, "* * * * *")
	}
}

func TestEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		wantErr  bool
		name     string
	}{
		{"", false, "empty input"},
		{"# only comment", false, "only comment"},
		{"   \n   \n   ", false, "only whitespace"},
		{"@yearly /usr/bin/backup.sh", true, "special syntax not supported"},
	}

	for _, tt := range tests {
		entries, err := ParseCrontab(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: ParseCrontab error = %v, want error=%v", tt.name, err, tt.wantErr)
		}
		if !tt.wantErr && len(entries) != 0 {
			t.Errorf("%s: expected no entries, got %d", tt.name, len(entries))
		}
	}
}

func TestMultipleFieldSeparators(t *testing.T) {
	tests := []struct {
		schedule string
		valid    bool
	}{
		{"1,2,3 * * * *", true},
		{"0-5,10-15 * * * *", true},
		{"0,10-20,50 * * * *", true},
		{"* 0-5,12-15 * * *", true},
	}

	for _, tt := range tests {
		err := ValidateSchedule(tt.schedule)
		if (err == nil) != tt.valid {
			t.Errorf("ValidateSchedule(%q) valid=%v, want=%v (err=%v)", tt.schedule, err == nil, tt.valid, err)
		}
	}
}

func TestPreserveNonEntryLines(t *testing.T) {
	text := `# DO NOT EDIT THIS FILE - edit the master and reinstall.
# (- installed on Tue Aug  4 00:50:15 2026)
# TBT lead-gen pipeline (system cron, no n8n)
PATH=/usr/local/lib/hermes-agent/venv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

0 6 * * * /root/tbt/run_ingest.sh >> /var/log/tbt/ingest.log 2>&1
# 0 9 * * * /usr/bin/commented-out.sh
0 */2 * * * /root/.hermes/scripts/tbt_enrich.sh >> /var/log/tbt/enrich.log 2>&1
`
	preserved := PreserveNonEntryLines(text)

	for _, want := range []string{
		"# DO NOT EDIT THIS FILE - edit the master and reinstall.",
		"# (- installed on Tue Aug  4 00:50:15 2026)",
		"# TBT lead-gen pipeline (system cron, no n8n)",
		"PATH=/usr/local/lib/hermes-agent/venv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"",
	} {
		found := false
		for _, p := range preserved {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("PreserveNonEntryLines missing %q; got %q", want, preserved)
		}
	}

	// Cron entries and commented-out cron jobs must NOT be preserved.
	for _, notWant := range []string{
		"0 6 * * * /root/tbt/run_ingest.sh",
		"# 0 9 * * * /usr/bin/commented-out.sh",
		"0 */2 * * * /root/.hermes/scripts/tbt_enrich.sh",
	} {
		for _, p := range preserved {
			if strings.Contains(p, notWant) {
				t.Errorf("PreserveNonEntryLines should not keep %q; got %q", notWant, preserved)
			}
		}
	}
}

func TestHasUntrackedActive(t *testing.T) {
	plain := Entry{Schedule: "* * * * *", Command: "/bin/true", Comment: "plain"}
	tracked := Entry{Schedule: "* * * * *", Command: "/bin/true", Comment: "tracked", Tracked: true}
	inactive := Entry{Schedule: "* * * * *", Command: "/bin/true", Comment: "plain", Inactive: true}

	if !HasUntrackedActive([]Entry{plain}) {
		t.Error("plain active job should be untracked")
	}
	if HasUntrackedActive([]Entry{tracked}) {
		t.Error("marked job should be tracked")
	}
	if HasUntrackedActive([]Entry{inactive}) {
		t.Error("inactive jobs should not count as untracked-active")
	}
	if HasUntrackedActive([]Entry{}) {
		t.Error("empty list should not be untracked-active")
	}
	// HasHgMarker works on the RAW comment (the marker is only stripped later
	// by unwrapEntry, which is why GetEntries records Tracked first).
	if !HasHgMarker("tracked " + hgMarker("/bin/true")) {
		t.Error("HasHgMarker should detect the marker on a raw comment")
	}
	if HasHgMarker(plain.Comment) {
		t.Error("HasHgMarker should not fire on plain comment")
	}
}
