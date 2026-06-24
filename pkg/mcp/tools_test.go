package mcp

import (
	"encoding/json"
	"testing"

	"github.com/tokenless/tokenless/pkg/budget"
	"github.com/tokenless/tokenless/pkg/config"
)

func newDispatcher(t *testing.T) (*Dispatcher, *budget.Store) {
	t.Helper()
	t.Setenv("TOKENLESS_CONFIG_DIR", t.TempDir())
	cfg := config.Default()
	store, err := budget.NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	b := budget.NewBudget(budget.Options{
		LoopThreshold:         cfg.LoopDetectionThreshold,
		MaxCostUSD:            cfg.MaxSessionCostUSD,
		MaxTokens:             cfg.MaxSessionTokens,
		PromptPricePerMillion: cfg.Pricing.PromptPerMillion,
		CostFn:                cfg.CostForTokens,
		Store:                 store,
	})
	return &Dispatcher{Cfg: cfg, Budget: b}, store
}

func TestDispatcherViewFileSlimMissingArgs(t *testing.T) {
	d, _ := newDispatcher(t)
	_, err := d.viewFileSlim(map[string]interface{}{})
	if err == nil || err.Code != -32602 {
		t.Errorf("expected invalid params error, got %+v", err)
	}
}

func TestDispatcherViewFileSlimMissingFile(t *testing.T) {
	d, _ := newDispatcher(t)
	res, err := d.viewFileSlim(map[string]interface{}{
		"file_path": "/nonexistent/path/to/foo.go",
	})
	if err != nil {
		t.Fatalf("expected ErrorResult, got error: %+v", err)
	}
	if !res.IsError {
		t.Error("expected isError=true for missing file")
	}
}

func TestDispatcherViewFileSlimGo(t *testing.T) {
	d, _ := newDispatcher(t)
	res, err := d.viewFileSlim(map[string]interface{}{
		"file_path":     "/home/eduardo/project/Gist/cmd/tokenless/main.go",
		"max_lines_body": 0,
	})
	if err != nil {
		t.Fatalf("unexpected: %+v", err)
	}
	if res.IsError {
		t.Errorf("unexpected error: %s", res.Content[0].Text)
	}
	if !contains(res.Content[0].Text, "slim_content") {
		t.Error("response missing slim_content")
	}
}

func TestDispatcherEnforceBudget(t *testing.T) {
	d, _ := newDispatcher(t)
	res, err := d.enforceBudget(map[string]interface{}{
		"session_id":       "test-1",
		"current_action":   "go test",
		"estimated_tokens": float64(100),
	})
	if err != nil {
		t.Fatalf("unexpected: %+v", err)
	}
	if !contains(res.Content[0].Text, "\"allowed\": true") {
		t.Errorf("expected allowed=true, got %s", res.Content[0].Text)
	}
}

func TestDispatcherEnforceBudgetMissingSession(t *testing.T) {
	d, _ := newDispatcher(t)
	_, err := d.enforceBudget(map[string]interface{}{})
	if err == nil || err.Code != -32602 {
		t.Errorf("expected invalid params, got %+v", err)
	}
}

func TestDispatcherEnforceBudgetTripped(t *testing.T) {
	d, _ := newDispatcher(t)
	args := map[string]interface{}{
		"session_id":       "trip-1",
		"current_action":   "go test ./...",
		"estimated_tokens": float64(10),
	}
	for i := 0; i < 3; i++ {
		_, _ = d.enforceBudget(args)
	}
	res, err := d.enforceBudget(args)
	if err != nil {
		t.Fatalf("unexpected: %+v", err)
	}
	if !res.IsError {
		t.Error("expected isError=true on trip")
	}
	if !contains(res.Content[0].Text, "\"tripped\": true") {
		t.Errorf("expected tripped=true, got %s", res.Content[0].Text)
	}
}

func TestDispatcherAlignContext(t *testing.T) {
	d, _ := newDispatcher(t)
	res, err := d.alignContext(map[string]interface{}{
		"system_prompts":      []interface{}{"rule 1"},
		"static_files_context": []interface{}{"file a"},
		"dynamic_input":        "the error",
	})
	if err != nil {
		t.Fatalf("unexpected: %+v", err)
	}
	if !contains(res.Content[0].Text, "blocks") {
		t.Error("aligner response missing blocks")
	}
}

func TestDispatcherAlignContextDisabled(t *testing.T) {
	d, _ := newDispatcher(t)
	d.Cfg.CacheAlignmentEnabled = false
	res, err := d.alignContext(map[string]interface{}{
		"system_prompts":      []interface{}{"rule 1"},
		"static_files_context": []interface{}{"file a"},
		"dynamic_input":        "the error",
	})
	if err != nil {
		t.Fatalf("unexpected: %+v", err)
	}
	if !contains(res.Content[0].Text, "rule 1") {
		t.Error("disabled mode should still concatenate content")
	}
}

func TestDispatcherUnknownTool(t *testing.T) {
	d, _ := newDispatcher(t)
	_, err := d.Handle("unknown", nil)
	if err == nil {
		t.Error("expected method-not-found error")
	}
}

func TestDispatcherFetchDiffNotGit(t *testing.T) {
	d, _ := newDispatcher(t)
	t.Setenv("TOKENLESS_CONFIG_DIR", t.TempDir())
	_, err := d.fetchDiff(map[string]interface{}{
		"cwd": "/nonexistent",
	})
	if err == nil {
		t.Error("expected internal error for non-git cwd")
	}
}

func TestStringList(t *testing.T) {
	if got := stringList([]interface{}{"a", "b", 1, "c"}); len(got) != 3 {
		t.Errorf("stringList = %v, want 3 items", got)
	}
	if got := stringList(nil); len(got) != 0 {
		t.Errorf("stringList(nil) = %v, want empty", got)
	}
	if got := stringList("not a list"); len(got) != 0 {
		t.Errorf("stringList(string) = %v, want empty", got)
	}
}

func TestJoinStrings(t *testing.T) {
	if got := joinStrings([]string{}, ","); got != "" {
		t.Errorf("empty list got %q", got)
	}
	if got := joinStrings([]string{"a", "b", "c"}, "-"); got != "a-b-c" {
		t.Errorf("join = %q, want a-b-c", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestDispatcherEnforceBudgetResultJSON(t *testing.T) {
	d, _ := newDispatcher(t)
	res, _ := d.enforceBudget(map[string]interface{}{
		"session_id":       "json-1",
		"current_action":   "ls",
		"estimated_tokens": float64(50),
	})
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &parsed); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if parsed["allowed"] != true {
		t.Error("expected allowed=true")
	}
}