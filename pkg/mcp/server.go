// Package mcp implements the JSON-RPC 2.0 stdio server for the Model Context Protocol.
package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// Server is a JSON-RPC 2.0 MCP server speaking over an arbitrary reader/writer pair.
type Server struct {
	mu     sync.Mutex
	reader io.Reader
	writer io.Writer
	tools  []Tool
	handle func(name string, args map[string]interface{}) (*ToolCallResult, *Error)
}

// NewServer builds a Server. in/out default to os.Stdin/os.Stdout when nil.
func NewServer(in io.Reader, out io.Writer, tools []Tool, handler func(name string, args map[string]interface{}) (*ToolCallResult, *Error)) *Server {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return &Server{
		reader: in,
		writer: out,
		tools:  tools,
		handle: handler,
	}
}

// Run blocks reading JSON-RPC requests and writing responses until EOF.
func (s *Server) Run() error {
	dec := json.NewDecoder(s.reader)
	enc := json.NewEncoder(s.writer)
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			s.writeErr(enc, nil, ErrParse())
			return nil
		}
		s.dispatch(enc, &req)
	}
}

func (s *Server) dispatch(enc *json.Encoder, req *Request) {
	resp := s.handleRequest(req)
	if resp == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := enc.Encode(resp); err != nil {
		fmt.Fprintln(os.Stderr, "tokenless: encode error:", err)
	}
}

func (s *Server) writeErr(enc *json.Encoder, id json.RawMessage, e *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = enc.Encode(&Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   e,
	})
}

func (s *Server) handleRequest(req *Request) *Response {
	switch req.Method {
	case "initialize":
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": ProtocolVersion,
				"serverInfo": map[string]string{
					"name":    ServerName,
					"version": ServerVersion,
				},
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
			},
		}
	case "ping":
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]string{"status": "pong"},
		}
	case "tools/list":
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"tools": s.tools,
			},
		}
	case "tools/call":
		var params ToolCallParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return &Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error:   ErrInvalidParams("tools/call params: " + err.Error()),
				}
			}
		}
		if params.Name == "" {
			return &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   ErrInvalidParams("missing tool name"),
			}
		}
		result, callErr := s.handle(params.Name, params.Arguments)
		if callErr != nil {
			return &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   callErr,
			}
		}
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}
	case "notifications/initialized", "notifications/cancelled":
		return nil
	default:
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   ErrMethodNotFound(req.Method),
		}
	}
}

// TextResult is a convenience helper for building a successful ToolCallResult.
func TextResult(text string) *ToolCallResult {
	return &ToolCallResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
	}
}

// ErrorResult is a convenience helper for building a failed ToolCallResult.
func ErrorResult(text string) *ToolCallResult {
	return &ToolCallResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
		IsError: true,
	}
}