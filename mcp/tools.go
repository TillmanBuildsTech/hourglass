package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TillmanBuildsTech/hourglass/cron"
)

type tool struct {
	name        string
	description string
	inputSchema map[string]interface{}
	handler     func(m *cron.Manager, args json.RawMessage) toolsCallResult
}

// toolOrder fixes tools/list output order (map iteration order is
// randomized in Go, and a stable order makes tool listings reproducible
// for clients/tests).
var toolOrder = []string{
	"list_cron_jobs",
	"create_cron_job",
	"update_cron_job",
	"delete_cron_job",
	"validate_cron_schedule",
}

func buildTools() map[string]tool {
	tools := []tool{
		{
			name: "list_cron_jobs",
			description: "List all cron jobs in the crontab, including each job's index " +
				"(needed for update_cron_job/delete_cron_job), schedule, command, comment, " +
				"active state, and last execution result if known. Always call this before " +
				"update_cron_job or delete_cron_job to get current indices, since indices " +
				"shift whenever a job is added or removed.",
			inputSchema: objectSchema(nil, nil),
			handler:     listCronJobs,
		},
		{
			name: "create_cron_job",
			description: "Add a new cron job to the crontab.",
			inputSchema: objectSchema(map[string]interface{}{
				"schedule": schemaString("Cron schedule in 5-field format: \"minute hour day month weekday\" (e.g. \"0 9 * * *\" for 9am daily)."),
				"command":  schemaString("The shell command to run."),
				"comment":  schemaString("Optional human-readable description stored alongside the job."),
			}, []string{"schedule", "command"}),
			handler: createCronJob,
		},
		{
			name: "update_cron_job",
			description: "Replace an existing cron job at the given index with a new schedule/command/comment. " +
				"Call list_cron_jobs first to find the correct index.",
			inputSchema: objectSchema(map[string]interface{}{
				"index":    schemaInteger("Zero-based index of the job to update, as returned by list_cron_jobs."),
				"schedule": schemaString("Cron schedule in 5-field format: \"minute hour day month weekday\"."),
				"command":  schemaString("The shell command to run."),
				"comment":  schemaString("Optional human-readable description stored alongside the job."),
				"inactive": schemaBoolean("If true, the job is written commented-out (disabled) rather than active."),
			}, []string{"index", "schedule", "command"}),
			handler: updateCronJob,
		},
		{
			name: "delete_cron_job",
			description: "Delete the cron job at the given index. Call list_cron_jobs first to find the correct index.",
			inputSchema: objectSchema(map[string]interface{}{
				"index": schemaInteger("Zero-based index of the job to delete, as returned by list_cron_jobs."),
			}, []string{"index"}),
			handler: deleteCronJob,
		},
		{
			name:        "validate_cron_schedule",
			description: "Check whether a 5-field cron schedule string is valid without writing anything.",
			inputSchema: objectSchema(map[string]interface{}{
				"schedule": schemaString("Cron schedule in 5-field format: \"minute hour day month weekday\"."),
			}, []string{"schedule"}),
			handler: validateCronSchedule,
		},
	}

	byName := make(map[string]tool, len(tools))
	for _, t := range tools {
		byName[t.name] = t
	}
	return byName
}

func objectSchema(properties map[string]interface{}, required []string) map[string]interface{} {
	if properties == nil {
		properties = map[string]interface{}{}
	}
	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func schemaString(description string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": description}
}

func schemaInteger(description string) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "description": description}
}

func schemaBoolean(description string) map[string]interface{} {
	return map[string]interface{}{"type": "boolean", "description": description}
}

// jobView mirrors main.go's Entry API shape so tool output looks the same
// whether an agent gets it from list_cron_jobs or the web UI's GET /api/cron.
type jobView struct {
	Index      int    `json:"index"`
	Schedule   string `json:"schedule"`
	Command    string `json:"command"`
	Comment    string `json:"comment,omitempty"`
	Active     bool   `json:"active"`
	LastRun    *int64 `json:"last_run_unix_ms,omitempty"`
	LastStatus string `json:"last_status,omitempty"`
	LastCode   int    `json:"last_exit_code,omitempty"`
}

func listCronJobs(m *cron.Manager, _ json.RawMessage) toolsCallResult {
	entries, err := m.GetEntries()
	if err != nil {
		if strings.Contains(err.Error(), "no crontab found") {
			return textResult(toJSON([]jobView{}))
		}
		return errorResult(fmt.Errorf("failed to read crontab: %w", err))
	}

	views := make([]jobView, len(entries))
	for i, e := range entries {
		views[i] = jobView{
			Index:    i,
			Schedule: e.Schedule,
			Command:  e.Command,
			Comment:  e.Comment,
			Active:   !e.Inactive,
		}

		if exec := m.GetLastExecution(e.Command); exec != nil {
			ts := exec.Timestamp.Unix() * 1000
			views[i].LastRun = &ts
			views[i].LastStatus = exec.Status
			views[i].LastCode = exec.ExitCode
		}
	}

	return textResult(toJSON(views))
}

type createArgs struct {
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Comment  string `json:"comment"`
}

func createCronJob(m *cron.Manager, args json.RawMessage) toolsCallResult {
	var a createArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return errorResult(fmt.Errorf("invalid arguments: %w", err))
	}
	if strings.TrimSpace(a.Schedule) == "" || strings.TrimSpace(a.Command) == "" {
		return errorResult(fmt.Errorf("schedule and command are required"))
	}

	entry := cron.Entry{Schedule: a.Schedule, Command: a.Command, Comment: a.Comment}
	if err := m.AddEntry(entry); err != nil {
		return errorResult(err)
	}

	return textResult(fmt.Sprintf("Created cron job: %q -> %q", a.Schedule, a.Command))
}

type updateArgs struct {
	Index    int    `json:"index"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Comment  string `json:"comment"`
	Inactive bool   `json:"inactive"`
}

func updateCronJob(m *cron.Manager, args json.RawMessage) toolsCallResult {
	var a updateArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return errorResult(fmt.Errorf("invalid arguments: %w", err))
	}
	if strings.TrimSpace(a.Schedule) == "" || strings.TrimSpace(a.Command) == "" {
		return errorResult(fmt.Errorf("schedule and command are required"))
	}

	entry := cron.Entry{Schedule: a.Schedule, Command: a.Command, Comment: a.Comment, Inactive: a.Inactive}
	if err := m.UpdateEntry(a.Index, entry); err != nil {
		return errorResult(err)
	}

	return textResult(fmt.Sprintf("Updated cron job at index %d", a.Index))
}

type indexArgs struct {
	Index int `json:"index"`
}

func deleteCronJob(m *cron.Manager, args json.RawMessage) toolsCallResult {
	var a indexArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return errorResult(fmt.Errorf("invalid arguments: %w", err))
	}

	if err := m.DeleteEntry(a.Index); err != nil {
		return errorResult(err)
	}

	return textResult(fmt.Sprintf("Deleted cron job at index %d", a.Index))
}

type scheduleArgs struct {
	Schedule string `json:"schedule"`
}

func validateCronSchedule(_ *cron.Manager, args json.RawMessage) toolsCallResult {
	var a scheduleArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return errorResult(fmt.Errorf("invalid arguments: %w", err))
	}

	if err := cron.ValidateSchedule(a.Schedule); err != nil {
		return textResult(fmt.Sprintf("invalid: %s", err.Error()))
	}
	return textResult("valid")
}

func toJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}
