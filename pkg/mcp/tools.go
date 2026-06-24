// Package mcp dispatch layer: maps tool names to handler functions.
package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/elbader17/gist/pkg/aligner"
	"github.com/elbader17/gist/pkg/ast"
	"github.com/elbader17/gist/pkg/budget"
	"github.com/elbader17/gist/pkg/config"
	"github.com/elbader17/gist/pkg/diff"
)

// DefaultTools returns the four tools this server exposes.
func DefaultTools() []Tool {
	return []Tool{
		{
			Name:        "view_file_slim",
			Description: "Read a file returning a syntactically pruned version with function bodies collapsed for token efficiency.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to prune",
					},
					"focus_functions": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Function names to keep expanded (not collapsed)",
					},
					"max_lines_body": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum lines of body to preserve per block (0 = collapse fully)",
						"default":     0,
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "enforce_budget",
			Description: "Circuit breaker. Tracks session tokens, cost and detects repeated actions to halt runaway loops.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id":       map[string]interface{}{"type": "string"},
					"current_action":   map[string]interface{}{"type": "string"},
					"estimated_tokens": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"session_id", "current_action", "estimated_tokens"},
			},
		},
		{
			Name:        "align_context_cache",
			Description: "Reorder prompt components into cache-friendly layers (system, static, history, dynamic).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"system_prompts": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"static_files_context": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"dynamic_input": map[string]interface{}{"type": "string"},
					"history":       map[string]interface{}{"type": "string"},
				},
				"required": []string{"system_prompts", "static_files_context", "dynamic_input"},
			},
		},
		{
			Name:        "fetch_diff_context",
			Description: "Summarize git diff semantically: changed files, modified functions, log-only / comment-only detection.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target_branch": map[string]interface{}{"type": "string"},
					"base":          map[string]interface{}{"type": "string"},
					"cwd":           map[string]interface{}{"type": "string"},
					"max_files":     map[string]interface{}{"type": "integer"},
				},
			},
		},
	}
}

// Dispatcher routes tool calls to the underlying packages.
type Dispatcher struct {
	Cfg    *config.Config
	Budget *budget.Budget
}

// Handle implements the handler signature passed to mcp.NewServer.
func (d *Dispatcher) Handle(name string, args map[string]interface{}) (*ToolCallResult, *Error) {
	switch name {
	case "view_file_slim":
		return d.viewFileSlim(args)
	case "enforce_budget":
		return d.enforceBudget(args)
	case "align_context_cache":
		return d.alignContext(args)
	case "fetch_diff_context":
		return d.fetchDiff(args)
	default:
		return nil, ErrMethodNotFound(name)
	}
}

func (d *Dispatcher) viewFileSlim(args map[string]interface{}) (*ToolCallResult, *Error) {
	rawPath, _ := args["file_path"].(string)
	if rawPath == "" {
		return nil, ErrInvalidParams("file_path is required")
	}
	path, err := filepath.Abs(rawPath)
	if err != nil {
		return nil, ErrInvalidParams("file_path invalid: " + err.Error())
	}

	focus := []string{}
	if list, ok := args["focus_functions"].([]interface{}); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				focus = append(focus, s)
			}
		}
	}
	maxLines := 0
	if v, ok := args["max_lines_body"].(float64); ok {
		maxLines = int(v)
	}

	res, err := ast.BuildSlim(path, focus, maxLines)
	if err != nil {
		return ErrorResult(fmt.Sprintf("view_file_slim error: %v", err)), nil
	}
	out, _ := json.MarshalIndent(res, "", "  ")
	return TextResult(string(out)), nil
}

func (d *Dispatcher) enforceBudget(args map[string]interface{}) (*ToolCallResult, *Error) {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return nil, ErrInvalidParams("session_id required")
	}
	action, _ := args["current_action"].(string)
	tokensF, _ := args["estimated_tokens"].(float64)
	tokens := int64(tokensF)

	status, err := d.Budget.Check(sessionID, action, tokens)
	if err != nil {
		return nil, ErrInternal(err.Error())
	}
	out, _ := json.MarshalIndent(status, "", "  ")
	if status.Tripped {
		return &ToolCallResult{
			Content: []ContentBlock{{Type: "text", Text: string(out)}},
			IsError: true,
		}, nil
	}
	return TextResult(string(out)), nil
}

func (d *Dispatcher) alignContext(args map[string]interface{}) (*ToolCallResult, *Error) {
	system := stringList(args["system_prompts"])
	staticFiles := stringList(args["static_files_context"])
	dynamic, _ := args["dynamic_input"].(string)
	history, _ := args["history"].(string)

	if !d.Cfg.CacheAlignmentEnabled {
		combined := append([]string{}, system...)
		combined = append(combined, staticFiles...)
		if history != "" {
			combined = append(combined, history)
		}
		if dynamic != "" {
			combined = append(combined, dynamic)
		}
		return TextResult(joinStrings(combined, "\n\n")), nil
	}

	payload := aligner.Align(system, staticFiles, dynamic, history)
	out, _ := json.MarshalIndent(payload, "", "  ")
	return TextResult(string(out)), nil
}

func (d *Dispatcher) fetchDiff(args map[string]interface{}) (*ToolCallResult, *Error) {
	target, _ := args["target_branch"].(string)
	base, _ := args["base"].(string)
	cwd, _ := args["cwd"].(string)
	maxFiles := 0
	if v, ok := args["max_files"].(float64); ok {
		maxFiles = int(v)
	}

	sd, err := diff.Fetch(diff.Options{
		TargetBranch: target,
		Base:         base,
		Cwd:          cwd,
		MaxFiles:     maxFiles,
	})
	if err != nil {
		return nil, ErrInternal(err.Error())
	}
	if err := diff.Enrich(cwd, sd.Base, sd.Files); err != nil {
		return nil, ErrInternal(err.Error())
	}
	out, _ := json.MarshalIndent(sd, "", "  ")
	return TextResult(string(out)), nil
}

func stringList(v interface{}) []string {
	out := []string{}
	if list, ok := v.([]interface{}); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func joinStrings(items []string, sep string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}