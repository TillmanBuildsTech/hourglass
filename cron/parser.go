package cron

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Entry struct {
	Schedule string
	Command  string
	Comment  string
	Inactive bool
	// Tracked is set by GetEntries before unwrapping: true when the raw
	// crontab line carried the Hourglass history marker (i.e. the job was
	// written by Hourglass and logs executions). The marker itself is stripped
	// from Comment by unwrapEntry, so this flag is the only way to tell a
	// tracked job from one installed outside Hourglass.
	Tracked bool `json:"-"`
}

func ParseCrontab(text string) ([]Entry, error) {
	var entries []Entry
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		inactive := false
		if strings.HasPrefix(line, "#") {
			// Check if this is a commented-out cron job
			commentedLine := strings.TrimSpace(line[1:])
			if isValidCronLine(commentedLine) {
				line = commentedLine
				inactive = true
			} else {
				continue
			}
		}

		// Skip crontab environment variable assignments (NAME=value).
		if envLine.MatchString(line) {
			continue
		}

		entry, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("parse error: %w", err)
		}
		if entry != nil {
			entry.Inactive = inactive
			entries = append(entries, *entry)
		}
	}
	return entries, nil
}

func isValidCronLine(line string) bool {
	parts := strings.Fields(line)
	if len(parts) < 6 {
		return false
	}
	// First field must look like a cron minute — a Wildcard, step pattern,
	// number, or comma/range list. The raw parser handles full validation;
	// this guard just prevents non-cron free-text lines from being consumed.
	return isCronField(parts[0])
}

func isCronField(s string) bool {
	return s == "*" || regexCronField.MatchString(s)
}

var regexCronField = regexp.MustCompile(`^[\d\-\*\/,]+$`)

var envLine = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*=`)


// PreserveNonEntryLines extracts the crontab lines that are NOT cron entries
// (environment variable assignments such as PATH=..., standalone comment
// lines, and blank lines) so a write round-trip through WriteCrontab keeps
// them. Without this, rewriting a crontab that sets e.g. PATH= drops the
// assignment and silently breaks every job that relied on it.
func PreserveNonEntryLines(text string) []string {
	var preserved []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			preserved = append(preserved, "")
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			// Standalone comment (not a commented-out cron job).
			commentedLine := strings.TrimSpace(trimmed[1:])
			if !isValidCronLine(commentedLine) {
				preserved = append(preserved, line)
			}
			continue
		}
		if envLine.MatchString(trimmed) {
			preserved = append(preserved, line)
		}
	}
	return preserved
}

// HasHgMarker reports whether the comment carries Hourglass's history-tracking
// marker ([[hg:<base64>]]), i.e. whether the entry was written by Hourglass
// and therefore logs executions to the history log. Only meaningful on RAW
// (not yet unwrapped) entries — unwrapEntry strips the marker.
func HasHgMarker(comment string) bool {
	_, ok := extractHgMarker(comment)
	return ok
}

// HasUntrackedActive reports whether any active (non-disabled) entry lacks
// Hourglass history tracking. Such entries run but never record a
// LastRun/LastStatus, because only wrapped commands append to the history
// log. Callers can rewrite the crontab to wrap them. Entries must come from
// GetEntries, which sets Tracked from the raw crontab line before unwrapping.
func HasUntrackedActive(entries []Entry) bool {
	for _, e := range entries {
		if !e.Inactive && !e.Tracked {
			return true
		}
	}
	return false
}

func parseLine(line string) (*Entry, error) {
	if strings.HasPrefix(line, "#") {
		return nil, nil
	}

	var comment string
	if idx := strings.Index(line, "#"); idx != -1 {
		comment = strings.TrimSpace(line[idx+1:])
		line = line[:idx]
	}

	parts := strings.Fields(line)
	if len(parts) < 6 {
		return nil, fmt.Errorf("invalid cron line format: %q", line)
	}

	schedule := strings.Join(parts[:5], " ")
	command := strings.Join(parts[5:], " ")

	if err := ValidateSchedule(schedule); err != nil {
		return nil, err
	}

	return &Entry{
		Schedule: schedule,
		Command:  command,
		Comment:  comment,
		Inactive: false,
	}, nil
}

func ValidateSchedule(schedule string) error {
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return fmt.Errorf("schedule must have 5 fields (minute hour day month weekday), got %d", len(parts))
	}

	ranges := []struct {
		field string
		min   int
		max   int
	}{
		{"minute", 0, 59},
		{"hour", 0, 23},
		{"day", 1, 31},
		{"month", 1, 12},
		{"weekday", 0, 7},
	}

	for i, r := range ranges {
		if err := validateField(parts[i], r.min, r.max); err != nil {
			return fmt.Errorf("%s: %w", r.field, err)
		}
	}

	return nil
}

func validateField(field string, min, max int) error {
	if field == "*" {
		return nil
	}

	if strings.Contains(field, ",") {
		for _, part := range strings.Split(field, ",") {
			if err := validateField(strings.TrimSpace(part), min, max); err != nil {
				return err
			}
		}
		return nil
	}

	if strings.Contains(field, "-") {
		parts := strings.Split(field, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid range: %q", field)
		}
		start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return fmt.Errorf("invalid range start: %q", parts[0])
		}
		end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return fmt.Errorf("invalid range end: %q", parts[1])
		}
		if start < min || start > max {
			return fmt.Errorf("range start %d outside bounds [%d, %d]", start, min, max)
		}
		if end < min || end > max {
			return fmt.Errorf("range end %d outside bounds [%d, %d]", end, min, max)
		}
		if start > end {
			return fmt.Errorf("range start %d > end %d", start, end)
		}
		return nil
	}

	if strings.Contains(field, "/") {
		parts := strings.Split(field, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid step: %q", field)
		}
		base := strings.TrimSpace(parts[0])
		if base != "*" && base != "" {
			if err := validateField(base, min, max); err != nil {
				return err
			}
		}
		step, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return fmt.Errorf("invalid step value: %q", parts[1])
		}
		if step <= 0 {
			return fmt.Errorf("step must be positive, got %d", step)
		}
		return nil
	}

	num, err := strconv.Atoi(field)
	if err != nil {
		return fmt.Errorf("invalid value: %q", field)
	}
	if num < min || num > max {
		return fmt.Errorf("value %d outside bounds [%d, %d]", num, min, max)
	}
	return nil
}

func StringifyCrontab(entries []Entry) string {
	var lines []string
	for _, e := range entries {
		if e.Inactive {
			line := "# " + e.Schedule + " " + escapePercent(e.Command)
			if e.Comment != "" {
				line += " # " + escapePercent(e.Comment)
			}
			lines = append(lines, line)
		} else {
			line := e.Schedule + " " + escapePercent(e.Command)
			if e.Comment != "" {
				line += " # " + escapePercent(e.Comment)
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// escapePercent escapes bare '%' characters for a crontab command field. Per
// crontab(5), cron treats an unescaped '%' as a newline - it splits the
// command there and feeds everything after it to the job's stdin instead of
// running it - unless the '%' is preceded by a backslash. wrapCommandForHistory
// embeds literal '%' characters (printf format specifiers, "date +%3N"), so
// without this, cron silently truncates every wrapped command right before
// the history-log write and never runs it.
func escapePercent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '%' {
			b.WriteByte('\\')
			b.WriteByte('%')
			i++
			continue
		}
		if s[i] == '%' {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
