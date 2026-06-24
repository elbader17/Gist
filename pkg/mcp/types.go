// Package mcp implements the JSON-RPC 2.0 Model Context Protocol server used by
// Gist over stdio.
//
// The Server type reads JSON-RPC requests from an io.Reader and writes
// responses to an io.Writer. It supports the MCP methods required by spec
// 2024-11-05:
//
//   - initialize
//   - ping
//   - tools/list
//   - tools/call
//
// Notifications (no id) are silently dropped per the spec.
package mcp

import (
	"encoding/json"
)

// ProtocolVersion is the MCP protocol version this server implements.
const ProtocolVersion = "2024-11-05"

// ServerName identifies this server to MCP clients.
const ServerName = "gist"

// ServerVersion is the Gist version string.
const ServerVersion = "0.2.0"

// Request is a JSON-RPC 2.0 request envelope.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response envelope.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Tool describes an MCP tool to clients.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolCallParams is the params field of a tools/call request.
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// ToolCallResult is the result field returned by handlers.
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a single content entry in a tool result.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Standard JSON-RPC error codes used by Gist.
const (
	CodeParseError          = -32700
	CodeMethodNotFound      = -32601
	CodeInvalidParams       = -32602
	CodeInternalError       = -32603
)

// ErrParse builds a parse error response.
func ErrParse() *Error { return &Error{Code: CodeParseError, Message: "Parse error"} }

// ErrMethodNotFound builds a method-not-found error.
func ErrMethodNotFound(m string) *Error {
	return &Error{Code: CodeMethodNotFound, Message: "Method not found: " + m}
}

// ErrInvalidParams builds an invalid-params error.
func ErrInvalidParams(msg string) *Error {
	return &Error{Code: CodeInvalidParams, Message: "Invalid params: " + msg}
}

// ErrInternal builds an internal-error response.
func ErrInternal(msg string) *Error {
	return &Error{Code: CodeInternalError, Message: "Internal error: " + msg}
}