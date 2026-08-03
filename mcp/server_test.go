package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/TillmanBuildsTech/hourglass/cron"
)

// fakeCrontabExecutor is an in-memory cron.Executor so tests never touch
// the real system crontab. Mirrors the fake used in main_test.go.
type fakeCrontabExecutor struct {
	crontab string
}

func (e *fakeCrontabExecutor) Execute(command string) (string, error) {
	if command == "crontab -l" {
		if e.crontab == "" {
			return "", fmt.Errorf("no crontab found or permission denied")
		}
		return e.crontab, nil
	}

	if strings.HasPrefix(command, "crontab - << 'EOF'") {
		start := strings.Index(command, "\n")
		end := strings.LastIndex(command, "\nEOF")
		if start == -1 || end == -1 || end < start {
			return "", fmt.Errorf("malformed heredoc")
		}
		e.crontab = command[start+1 : end]
		return "", nil
	}

	if command == "crontab -r" {
		e.crontab = ""
		return "", nil
	}

	return "", nil
}

func newTestServer() *Server {
	m := cron.NewManagerWithExecutor(&fakeCrontabExecutor{})
	return NewServer(m, "test-version")
}

// call sends a single JSON-RPC request line through Serve and returns the
// decoded response. Serve is fed exactly one line (io.EOF ends the loop),
// which is enough to exercise one request/response round trip per call.
func call(t *testing.T, s *Server, id int, method string, params interface{}) response {
	t.Helper()

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}

	line, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var out bytes.Buffer
	if err := s.Serve(bytes.NewReader(append(line, '\n')), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", out.String(), err)
	}
	return resp
}

func callTool(t *testing.T, s *Server, name string, args interface{}) toolsCallResult {
	t.Helper()

	resp := call(t, s, 1, "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		t.Fatalf("tools/call %s returned RPC error: %+v", name, resp.Error)
	}

	// Round-trip through JSON since resp.Result is an interface{}.
	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var result toolsCallResult
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return result
}

func TestInitialize(t *testing.T) {
	s := newTestServer()
	resp := call(t, s, 1, "initialize", map[string]interface{}{"protocolVersion": "2024-11-05"})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	b, _ := json.Marshal(resp.Result)
	var result initializeResult
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("expected protocolVersion echoed back, got %q", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "hourglass" {
		t.Errorf("expected server name hourglass, got %q", result.ServerInfo.Name)
	}
	if result.ServerInfo.Version != "test-version" {
		t.Errorf("expected server version test-version, got %q", result.ServerInfo.Version)
	}
	if result.Capabilities.Tools == nil {
		t.Error("expected tools capability to be advertised")
	}
}

func TestInitializeDefaultsProtocolVersion(t *testing.T) {
	s := newTestServer()
	resp := call(t, s, 1, "initialize", nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	b, _ := json.Marshal(resp.Result)
	var result initializeResult
	json.Unmarshal(b, &result)

	if result.ProtocolVersion != defaultProtocolVersion {
		t.Errorf("expected default protocolVersion %q, got %q", defaultProtocolVersion, result.ProtocolVersion)
	}
}

func TestNotificationGetsNoResponse(t *testing.T) {
	s := newTestServer()

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	line, _ := json.Marshal(req)

	var out bytes.Buffer
	if err := s.Serve(bytes.NewReader(append(line, '\n')), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("expected no response to a notification, got %q", out.String())
	}
}

func TestUnknownMethod(t *testing.T) {
	s := newTestServer()
	resp := call(t, s, 1, "does/not/exist", nil)

	if resp.Error == nil {
		t.Fatal("expected an error for an unknown method")
	}
	if resp.Error.Code != errMethodNotFound {
		t.Errorf("expected code %d, got %d", errMethodNotFound, resp.Error.Code)
	}
}

func TestToolsList(t *testing.T) {
	s := newTestServer()
	resp := call(t, s, 1, "tools/list", nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	b, _ := json.Marshal(resp.Result)
	var result toolsListResult
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Tools) != len(toolOrder) {
		t.Fatalf("expected %d tools, got %d", len(toolOrder), len(result.Tools))
	}

	names := make(map[string]bool)
	for _, tl := range result.Tools {
		names[tl.Name] = true
		if tl.Description == "" {
			t.Errorf("tool %q has no description", tl.Name)
		}
		if tl.InputSchema["type"] != "object" {
			t.Errorf("tool %q schema type = %v, want object", tl.Name, tl.InputSchema["type"])
		}
	}
	for _, want := range toolOrder {
		if !names[want] {
			t.Errorf("expected tool %q in tools/list output", want)
		}
	}
}

func TestCreateListUpdateDeleteCronJob(t *testing.T) {
	s := newTestServer()

	created := callTool(t, s, "create_cron_job", map[string]interface{}{
		"schedule": "0 9 * * *",
		"command":  "/usr/bin/backup.sh",
		"comment":  "nightly backup",
	})
	if created.IsError {
		t.Fatalf("create_cron_job returned an error: %s", created.Content[0].Text)
	}

	listed := callTool(t, s, "list_cron_jobs", map[string]interface{}{})
	if listed.IsError {
		t.Fatalf("list_cron_jobs returned an error: %s", listed.Content[0].Text)
	}

	var jobs []jobView
	if err := json.Unmarshal([]byte(listed.Content[0].Text), &jobs); err != nil {
		t.Fatalf("unmarshal jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Schedule != "0 9 * * *" || jobs[0].Command != "/usr/bin/backup.sh" || jobs[0].Comment != "nightly backup" {
		t.Errorf("unexpected job: %+v", jobs[0])
	}
	if !jobs[0].Active {
		t.Error("expected newly created job to be active")
	}

	updated := callTool(t, s, "update_cron_job", map[string]interface{}{
		"index":    0,
		"schedule": "30 10 * * *",
		"command":  "/usr/bin/backup.sh --full",
		"comment":  "nightly backup",
	})
	if updated.IsError {
		t.Fatalf("update_cron_job returned an error: %s", updated.Content[0].Text)
	}

	listed = callTool(t, s, "list_cron_jobs", map[string]interface{}{})
	json.Unmarshal([]byte(listed.Content[0].Text), &jobs)
	if len(jobs) != 1 || jobs[0].Schedule != "30 10 * * *" || jobs[0].Command != "/usr/bin/backup.sh --full" {
		t.Errorf("update did not apply, got: %+v", jobs)
	}

	deleted := callTool(t, s, "delete_cron_job", map[string]interface{}{"index": 0})
	if deleted.IsError {
		t.Fatalf("delete_cron_job returned an error: %s", deleted.Content[0].Text)
	}

	listed = callTool(t, s, "list_cron_jobs", map[string]interface{}{})
	json.Unmarshal([]byte(listed.Content[0].Text), &jobs)
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs after delete, got %d", len(jobs))
	}
}

func TestCreateCronJobRejectsInvalidSchedule(t *testing.T) {
	s := newTestServer()

	result := callTool(t, s, "create_cron_job", map[string]interface{}{
		"schedule": "99 * * * *",
		"command":  "echo hi",
	})
	if !result.IsError {
		t.Fatal("expected an error for an out-of-range schedule")
	}
}

func TestDeleteCronJobOutOfRange(t *testing.T) {
	s := newTestServer()

	result := callTool(t, s, "delete_cron_job", map[string]interface{}{"index": 5})
	if !result.IsError {
		t.Fatal("expected an error deleting an out-of-range index")
	}
}

func TestValidateCronSchedule(t *testing.T) {
	s := newTestServer()

	valid := callTool(t, s, "validate_cron_schedule", map[string]interface{}{"schedule": "*/5 * * * *"})
	if valid.IsError || valid.Content[0].Text != "valid" {
		t.Errorf("expected schedule to be valid, got %+v", valid)
	}

	invalid := callTool(t, s, "validate_cron_schedule", map[string]interface{}{"schedule": "70 * * * *"})
	if invalid.IsError {
		t.Fatalf("validate_cron_schedule should report invalidity as text, not an RPC/tool error: %+v", invalid)
	}
	if !strings.HasPrefix(invalid.Content[0].Text, "invalid:") {
		t.Errorf("expected invalid schedule message, got %q", invalid.Content[0].Text)
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	s := newTestServer()
	resp := call(t, s, 1, "tools/call", map[string]interface{}{"name": "does_not_exist", "arguments": map[string]interface{}{}})

	if resp.Error == nil {
		t.Fatal("expected an RPC error for an unknown tool")
	}
	if resp.Error.Code != errInvalidParams {
		t.Errorf("expected code %d, got %d", errInvalidParams, resp.Error.Code)
	}
}

func TestParseErrorRecoveredID(t *testing.T) {
	s := newTestServer()

	var out bytes.Buffer
	if err := s.Serve(strings.NewReader("not json\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", out.String(), err)
	}
	if resp.Error == nil || resp.Error.Code != errParseError {
		t.Errorf("expected parse error response, got %+v", resp)
	}
}

func TestMultipleRequestsInOneStream(t *testing.T) {
	s := newTestServer()

	reqs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	}

	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(strings.Join(reqs, "\n")+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses (notification suppressed), got %d: %q", len(lines), out.String())
	}
}
