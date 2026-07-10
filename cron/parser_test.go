package cron

import (
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
