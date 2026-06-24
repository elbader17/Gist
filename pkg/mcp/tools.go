// Package mcp dispatch layer: maps tool names to handler functions.
package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/elbader17/gist/pkg/aligner"
	"github.com/elbader17/gist/pkg/ast"
	"github.com/elbader17/gist/pkg/budget"
	"github.com/elbader17/gist/pkg/cache"
	"github.com/elbader17/gist/pkg/config"
	"github.com/elbader17/gist/pkg/diff"
	"github.com/elbader17/gist/pkg/metrics"
	"github.com/elbader17/gist/pkg/squeeze"
	"github.com/elbader17/gist/pkg/tokenizer"
)

// DefaultTools returns the six tools this server exposes.
func DefaultTools() []Tool {
	return []Tool{
		{
			Name:        "view_file_slim",
			Description: "Read a file returning a syntactically pruned version with function bodies collapsed for token efficiency. Go files use AST pruning; Python/JS/TS/Rust/Java/C/C++/Ruby use signature-only pruning; structured formats (json/yaml/toml/markdown) pass through.",
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
			Description: "Reorder prompt components into cache-friendly layers (system, static, history, dynamic) and return a cache-friendly markdown with stable layer hashes.",
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
			Description: "Summarize git diff semantically: changed files, modified functions, log-only / comment-only detection. Enrichment runs in parallel.",
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
		{
			Name:        "squeeze_context",
			Description: "One-call optimizer: prunes listed files (cached), aligns layers, enforces a token cap, and returns a single ready-to-send prompt plus savings metrics.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id":     map[string]interface{}{"type": "string"},
					"system_prompts": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"static_files": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path":            map[string]interface{}{"type": "string"},
								"focus_functions": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
							},
							"required": []string{"path"},
						},
					},
					"history":      map[string]interface{}{"type": "string"},
					"dynamic_input": map[string]interface{}{"type": "string"},
					"max_tokens":   map[string]interface{}{"type": "integer", "default": 0},
					"encoding":     map[string]interface{}{"type": "string", "enum": []string{"cl100k_base", "o200k_base", "p50k_base"}},
				},
			},
		},
		{
			Name:        "report_savings",
			Description: "Return cumulative token-savings telemetry across sessions and tools.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]interface{}{"type": "string", "description": "If set, scope report to one session; otherwise aggregate across all."},
				},
			},
		},
	}
}

// Dispatcher routes tool calls to the underlying packages.
type Dispatcher struct {
	Cfg     *config.Config
	Budget  *budget.Budget
	Cache   *cache.Cache
	Metrics *metrics.Recorder
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
	case "squeeze_context":
		return d.squeezeContext(args)
	case "report_savings":
		return d.reportSavings(args)
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

	tok := tokenizer.New(tokenizer.Encoding(d.Cfg.DefaultTokenizerEncoding))
	var res *ast.SkeletonResult
	var cached bool
	if d.Cache != nil {
		if e, ok := d.Cache.Get(path); ok {
			res = &ast.SkeletonResult{
				FilePath:     path,
				Language:     ast.DetectLanguage(path),
				Slim:         e.Slim,
				OriginalSize: e.OriginalBytes,
			}
			cached = true
		}
	}
	if res == nil {
		built, err := ast.BuildSlim(path, focus, maxLines)
		if err != nil {
			return ErrorResult(fmt.Sprintf("view_file_slim error: %v", err)), nil
		}
		res = built
		if d.Cache != nil && res.Slim != "" {
			d.Cache.Put(path, res.Slim, tok.CountString(res.Slim))
		}
	}

	out, _ := json.MarshalIndent(res, "", "  ")
	if d.Metrics != nil {
		sessionID, _ := args["session_id"].(string)
		originalTokens := int(res.OriginalSize / 4)
		outputTokens := tok.CountString(res.Slim)
		if originalTokens < outputTokens {
			originalTokens = outputTokens
		}
		d.Metrics.Record(sessionID, "view_file_slim", originalTokens, outputTokens)
	}
	if cached {
		// Surface cache hit as a small hint inside the result.
		wrapped := map[string]interface{}{}
		_ = json.Unmarshal(out, &wrapped)
		wrapped["cache_hit"] = true
		out2, _ := json.MarshalIndent(wrapped, "", "  ")
		return TextResult(string(out2)), nil
	}
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
	if err := diff.EnrichParallel(cwd, sd.Base, sd.Files, 0); err != nil {
		return nil, ErrInternal(err.Error())
	}
	out, _ := json.MarshalIndent(sd, "", "  ")
	return TextResult(string(out)), nil
}

func (d *Dispatcher) squeezeContext(args map[string]interface{}) (*ToolCallResult, *Error) {
	sessionID, _ := args["session_id"].(string)

	systemPrompts := stringList(args["system_prompts"])

	staticFiles := []squeeze.FileSource{}
	if list, ok := args["static_files"].([]interface{}); ok {
		for _, item := range list {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			raw, _ := m["path"].(string)
			if raw == "" {
				continue
			}
			abs, err := filepath.Abs(raw)
			if err != nil {
				continue
			}
			fs := squeeze.FileSource{Path: abs}
			if focus, ok := m["focus_functions"].([]interface{}); ok {
				for _, f := range focus {
					if s, ok := f.(string); ok {
						fs.FocusFunctions = append(fs.FocusFunctions, s)
					}
				}
			}
			staticFiles = append(staticFiles, fs)
		}
	}

	history, _ := args["history"].(string)
	dynamic, _ := args["dynamic_input"].(string)
	maxTokens := 0
	if v, ok := args["max_tokens"].(float64); ok {
		maxTokens = int(v)
	}
	enc := tokenizer.Encoding(d.Cfg.DefaultTokenizerEncoding)
	if e, ok := args["encoding"].(string); ok && e != "" {
		enc = tokenizer.Encoding(e)
	}

	result, err := squeeze.Squeeze(squeeze.Options{
		SessionID:     sessionID,
		SystemPrompts: systemPrompts,
		StaticFiles:   staticFiles,
		History:       history,
		DynamicInput:  dynamic,
		MaxTokens:     maxTokens,
		Encoding:      enc,
		Cache:         d.Cache,
	})
	if err != nil {
		return nil, ErrInternal(err.Error())
	}

	if d.Metrics != nil {
		d.Metrics.Record(sessionID, "squeeze_context", result.OriginalTokens, result.TotalTokens)
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return TextResult(string(out)), nil
}

func (d *Dispatcher) reportSavings(args map[string]interface{}) (*ToolCallResult, *Error) {
	if d.Metrics == nil {
		return TextResult(`{"error": "metrics recorder not configured"}`), nil
	}
	sessionID, _ := args["session_id"].(string)
	var payload interface{}
	if sessionID != "" {
		s, ok := d.Metrics.Session(sessionID)
		if !ok {
			return TextResult(`{"session_id": "` + sessionID + `", "found": false}`), nil
		}
		payload = s
	} else {
		payload = d.Metrics.Aggregate()
	}
	out, _ := json.MarshalIndent(payload, "", "  ")
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