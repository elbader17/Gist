package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func newTestServer() *Server {
	tools := []Tool{
		{Name: "echo", Description: "Echoes input", InputSchema: map[string]interface{}{"type": "object"}},
	}
	handler := func(name string, args map[string]interface{}) (*ToolCallResult, *Error) {
		switch name {
		case "echo":
			msg, _ := args["msg"].(string)
			return TextResult("echo: " + msg), nil
		case "fail":
			return ErrorResult("intentional failure"), nil
		}
		return nil, ErrMethodNotFound(name)
	}
	return NewServer(nil, nil, tools, handler)
}

func TestServerInitialize(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if err := srv.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q", resp.JSONRPC)
	}
}

func TestServerToolsList(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	_ = srv.Run()

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result not map: %T", resp.Result)
	}
	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("tools not array: %T", result["tools"])
	}
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
}

func TestServerToolsCallSuccess(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hi"}}}`)
	_ = srv.Run()

	var resp Response
	_ = json.Unmarshal(buf.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	res, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result not map: %T", resp.Result)
	}
	content, ok := res["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("no content: %+v", res)
	}
}

func TestServerToolsCallError(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fail"}}`)
	_ = srv.Run()

	var resp Response
	_ = json.Unmarshal(buf.Bytes(), &resp)
	res, ok := resp.Result.(map[string]interface{})
	if !ok || res["isError"] != true {
		t.Errorf("expected isError=true, got %+v", resp.Result)
	}
}

func TestServerToolsCallUnknownTool(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"unknown"}}`)
	_ = srv.Run()

	var resp Response
	_ = json.Unmarshal(buf.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("expected method-not-found, got %+v", resp.Error)
	}
}

func TestServerUnknownMethod(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","id":6,"method":"foo/bar"}`)
	_ = srv.Run()

	var resp Response
	_ = json.Unmarshal(buf.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("expected method-not-found, got %+v", resp.Error)
	}
}

func TestServerInvalidJSON(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{not valid json`)
	_ = srv.Run()

	if !strings.Contains(buf.String(), "Parse error") {
		t.Errorf("expected parse error in output, got %q", buf.String())
	}
}

func TestServerPing(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	_ = srv.Run()

	var resp Response
	_ = json.Unmarshal(buf.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
}

func TestServerMultipleRequests(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	input := `{"jsonrpc":"2.0","id":1,"method":"ping"}
{"jsonrpc":"2.0","id":2,"method":"ping"}
{"jsonrpc":"2.0","id":3,"method":"ping"}
`
	srv.reader = strings.NewReader(input)
	_ = srv.Run()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 responses, got %d: %v", len(lines), lines)
	}
}

func TestTextResult(t *testing.T) {
	r := TextResult("hello")
	if len(r.Content) != 1 || r.Content[0].Text != "hello" {
		t.Errorf("unexpected TextResult: %+v", r)
	}
	if r.IsError {
		t.Error("TextResult should not be an error")
	}
}

func TestErrorResult(t *testing.T) {
	r := ErrorResult("boom")
	if !r.IsError {
		t.Error("ErrorResult should set IsError=true")
	}
}

func TestErrorHelpers(t *testing.T) {
	if ErrParse().Code != -32700 {
		t.Error("ErrParse wrong code")
	}
	if ErrMethodNotFound("x").Code != -32601 {
		t.Error("ErrMethodNotFound wrong code")
	}
	if ErrInvalidParams("x").Code != -32602 {
		t.Error("ErrInvalidParams wrong code")
	}
	if ErrInternal("x").Code != -32603 {
		t.Error("ErrInternal wrong code")
	}
}

func TestServerMissingToolName(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{}}`)
	_ = srv.Run()

	var resp Response
	_ = json.Unmarshal(buf.Bytes(), &resp)
	if resp.Error == nil {
		t.Error("expected error for missing tool name")
	}
}

func TestServerInvalidParamsJSON(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":"not-an-object"}`)
	_ = srv.Run()

	var resp Response
	_ = json.Unmarshal(buf.Bytes(), &resp)
	if resp.Error == nil {
		t.Error("expected error for invalid params structure")
	}
}

func TestServerEOF(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader("")
	if err := srv.Run(); err != nil {
		t.Errorf("EOF should not produce error, got %v", err)
	}
}

func TestServerNotificationsIgnored(t *testing.T) {
	srv := newTestServer()
	var buf bytes.Buffer
	srv.writer = &buf
	srv.reader = strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	_ = srv.Run()

	if buf.Len() != 0 {
		t.Errorf("notifications should not write a response, got %q", buf.String())
	}
}