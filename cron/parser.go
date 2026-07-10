package cron

import (
	"fmt"
	"strconv"
	"strings"
)

type Entry struct {
	Schedule string
	Command  string
	Comment  string
	Inactive bool
}

func ParseCrontab(text string) ([]Entry, error) {
	var entries []Entry
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entry, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("parse error: %w", err)
		}
		if entry != nil {
			entries = append(entries, *entry)
		}
	}
	return entries, nil
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
			lines = append(lines, "# "+e.Schedule+" "+e.Command)
		} else {
			line := e.Schedule + " " + e.Command
			if e.Comment != "" {
				line += " # " + e.Comment
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
