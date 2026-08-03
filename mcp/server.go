package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/TillmanBuildsTech/hourglass/cron"
)

// maxLineBytes bounds a single stdio message. Crontab payloads are small;
// this is generous headroom, not a real limit on job count.
const maxLineBytes = 4 << 20

// Server dispatches MCP JSON-RPC requests against a cron.Manager.
type Server struct {
	cronManager *cron.Manager
	version     string
	tools       map[string]tool
}

// NewServer builds an MCP server backed by cronManager. version is
// reported to clients as the server's version (Hourglass's own VERSION).
func NewServer(cronManager *cron.Manager, version string) *Server {
	s := &Server{cronManager: cronManager, version: version}
	s.tools = buildTools()
	return s
}

// Serve reads newline-delimited JSON-RPC requests from in and writes
// newline-delimited responses to out until in is exhausted or a read
// error occurs. Per the MCP stdio transport, only JSON-RPC messages may
// go to out - diagnostics must go elsewhere, so this logs to stderr via
// the standard log package.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	w := bufio.NewWriter(out)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytesTrimSpace(line)) == 0 {
			continue
		}

		resp, ok := s.handleLine(line)
		if !ok {
			// Notification: no response is sent.
			continue
		}

		if err := writeResponse(w, resp); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}

	return scanner.Err()
}

func bytesTrimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpace(b[start]) {
		start++
	}
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func writeResponse(w *bufio.Writer, resp response) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}

// handleLine parses and dispatches one JSON-RPC message. ok is false for
// notifications (no "id"), which must not produce a response.
func (s *Server) handleLine(line []byte) (resp response, ok bool) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return response{
			JSONRPC: jsonRPCVersion,
			Error:   &rpcError{Code: errParseError, Message: "parse error: " + err.Error()},
		}, true
	}

	isNotification := req.ID == nil

	result, rpcErr := s.dispatch(req)

	if isNotification {
		if rpcErr != nil {
			log.Printf("mcp: error handling notification %q: %s", req.Method, rpcErr.Message)
		}
		return response{}, false
	}

	resp = response{JSONRPC: jsonRPCVersion, ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	return resp, true
}

func (s *Server) dispatch(req request) (interface{}, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.Params)
	case "notifications/initialized", "initialized":
		return nil, nil
	case "ping":
		return struct{}{}, nil
	case "tools/list":
		return s.handleToolsList()
	case "tools/call":
		return s.handleToolsCall(req.Params)
	default:
		return nil, &rpcError{Code: errMethodNotFound, Message: "method not found: " + req.Method}
	}
}

func (s *Server) handleInitialize(params json.RawMessage) (interface{}, *rpcError) {
	protocolVersion := defaultProtocolVersion
	if len(params) > 0 {
		var p initializeParams
		if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
			protocolVersion = p.ProtocolVersion
		}
	}

	return initializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities:    serverCapabilities{Tools: &toolsCapability{}},
		ServerInfo:      serverInfo{Name: "hourglass", Version: s.version},
	}, nil
}

func (s *Server) handleToolsList() (interface{}, *rpcError) {
	descriptors := make([]toolDescriptor, 0, len(s.tools))
	for _, name := range toolOrder {
		t, ok := s.tools[name]
		if !ok {
			continue
		}
		descriptors = append(descriptors, toolDescriptor{
			Name:        t.name,
			Description: t.description,
			InputSchema: t.inputSchema,
		})
	}
	return toolsListResult{Tools: descriptors}, nil
}

func (s *Server) handleToolsCall(params json.RawMessage) (interface{}, *rpcError) {
	var p toolsCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: "invalid params: " + err.Error()}
	}

	t, ok := s.tools[p.Name]
	if !ok {
		return nil, &rpcError{Code: errInvalidParams, Message: "unknown tool: " + p.Name}
	}

	args := p.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}

	return t.handler(s.cronManager, args), nil
}
