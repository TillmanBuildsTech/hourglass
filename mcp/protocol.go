// Package mcp implements a minimal Model Context Protocol server over
// stdio, letting AI agents (Claude Desktop, Claude Code, etc.) list, add,
// edit, and delete cron jobs through the same cron.Manager the HTTP API
// uses. It hand-rolls the JSON-RPC 2.0 framing rather than pulling in an
// SDK, keeping Hourglass a dependency-free single binary.
package mcp

import "encoding/json"

const jsonRPCVersion = "2.0"

// defaultProtocolVersion is advertised when the client's initialize
// request omits a protocolVersion. When present, the client's version is
// echoed back instead - Hourglass's tool set has no version-specific
// behavior, so it can safely negotiate whatever the client proposes.
const defaultProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 standard error codes.
const (
	errParseError     = -32700
	errMethodNotFound = -32601
	errInvalidParams  = -32602
)

// request's ID is a *json.RawMessage so presence can be distinguished from
// absence: a JSON-RPC notification omits "id" entirely and must not
// receive a response, whereas a request has an "id" (even if null).
type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
}

type serverCapabilities struct {
	Tools *toolsCapability `json:"tools,omitempty"`
}

type toolsCapability struct{}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsListResult struct {
	Tools []toolDescriptor `json:"tools"`
}

type toolDescriptor struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolsCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func textResult(text string) toolsCallResult {
	return toolsCallResult{Content: []contentBlock{{Type: "text", Text: text}}}
}

func errorResult(err error) toolsCallResult {
	return toolsCallResult{Content: []contentBlock{{Type: "text", Text: err.Error()}}, IsError: true}
}
