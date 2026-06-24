package budget

import (
	"strings"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Hello World  ", "hello world"},
		{"", ""},
		{"ABC", "abc"},
	}
	for _, c := range cases {
		if got := normalize(c.in); got != c.want {
			t.Errorf("normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeTruncates(t *testing.T) {
	long := ""
	for i := 0; i < 500; i++ {
		long += "a"
	}
	got := normalize(long)
	if len(got) != 200 {
		t.Errorf("normalize should truncate to 200 chars, got %d", len(got))
	}
}

func TestDetectLoopNoMatch(t *testing.T) {
	actions := []Action{
		{Action: "build", Timestamp: time.Now()},
		{Action: "test", Timestamp: time.Now()},
	}
	act, count := detectLoop(actions, "deploy")
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if act != "" {
		t.Errorf("act = %q, want empty", act)
	}
}

func TestDetectLoopMatch(t *testing.T) {
	now := time.Now()
	actions := []Action{
		{Action: "go test", Timestamp: now},
		{Action: "go test", Timestamp: now},
		{Action: "go test", Timestamp: now},
		{Action: "go test", Timestamp: now},
	}
	act, count := detectLoop(actions, "go test")
	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}
	if act != "go test" {
		t.Errorf("act = %q, want go test", act)
	}
}

func TestDetectLoopEmptyCurrent(t *testing.T) {
	act, count := detectLoop(nil, "")
	if act != "" || count != 0 {
		t.Errorf("empty current should return 0,0 got %q,%d", act, count)
	}
}

func TestStoreGetOrCreateIdempotent(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)

	s1 := store.GetOrCreate("alpha")
	s2 := store.GetOrCreate("alpha")
	if s1 != s2 {
		t.Error("GetOrCreate should return the same pointer")
	}
	if s1.ID != "alpha" {
		t.Errorf("id = %q", s1.ID)
	}
}

func TestStoreGetOrCreateNew(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)

	s1 := store.GetOrCreate("beta")
	if s1 == nil {
		t.Fatal("nil session")
	}
	if s1.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
	if s1.RecentActions == nil {
		t.Error("RecentActions should be initialized")
	}
}

func TestStoreGetMissing(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	_, ok := store.Get("missing")
	if ok {
		t.Error("missing session should return ok=false")
	}
}

func TestStoreSaveAndLoad(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	sess := store.GetOrCreate("persist-1")
	sess.TotalTokens = 100
	sess.TotalCostUSD = 0.05

	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	store2 := mustStore(t)
	loaded, ok := store2.Get("persist-1")
	if !ok {
		t.Fatal("session not loaded")
	}
	if loaded.TotalTokens != 100 || loaded.TotalCostUSD != 0.05 {
		t.Errorf("loaded session state mismatch: %+v", loaded)
	}
}

func TestBudgetCheckBasic(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         3,
		MaxCostUSD:            1.0,
		MaxTokens:             10000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	status, err := b.Check("s1", "build", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Allowed {
		t.Error("first action should be allowed")
	}
	if status.TotalTokens != 100 {
		t.Errorf("tokens = %d, want 100", status.TotalTokens)
	}
}

func TestBudgetCheckEmptySessionID(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         3,
		MaxCostUSD:            1.0,
		MaxTokens:             10000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})
	_, err := b.Check("", "x", 1)
	if err == nil {
		t.Error("expected error for empty session_id")
	}
}

func TestBudgetCheckNoStore(t *testing.T) {
	b := NewBudget(Options{
		LoopThreshold:         3,
		MaxCostUSD:            1.0,
		MaxTokens:             10000,
		PromptPricePerMillion: 3.0,
	})
	_, err := b.Check("x", "y", 1)
	if err == nil {
		t.Error("expected error when store is nil")
	}
}

func TestBudgetCheckTrippedByLoop(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         3,
		MaxCostUSD:            100,
		MaxTokens:             1_000_000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	for i := 0; i < 3; i++ {
		_, _ = b.Check("loop-1", "go test ./...", 100)
	}

	status, err := b.Check("loop-1", "go test ./...", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Tripped {
		t.Error("expected tripped on 4th repeat")
	}
	if !status.LoopDetected {
		t.Error("expected loop_detected")
	}
	if status.RepeatedCount < 3 {
		t.Errorf("repeated count = %d", status.RepeatedCount)
	}
}

func TestBudgetCheckTrippedByCost(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         100,
		MaxCostUSD:            0.001,
		MaxTokens:             1_000_000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	_, _ = b.Check("cost-1", "build", 1000)
	status, _ := b.Check("cost-1", "build", 1000)
	if !status.Tripped {
		t.Error("expected tripped by cost")
	}
	if !strings.Contains(status.Reason, "cost") {
		t.Errorf("reason should mention cost: %q", status.Reason)
	}
}

func TestBudgetCheckTrippedByTokens(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         100,
		MaxCostUSD:            1_000_000,
		MaxTokens:             5000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	_, _ = b.Check("tok-1", "x", 3000)
	status, _ := b.Check("tok-1", "x", 3000)
	if !status.Tripped {
		t.Error("expected tripped by tokens")
	}
}

func TestBudgetCheckNormalizesAction(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         3,
		MaxCostUSD:            100,
		MaxTokens:             1_000_000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	for _, action := range []string{"  Go Test  ", "go test", "GO test"} {
		_, _ = b.Check("norm-1", action, 100)
	}
	status, _ := b.Check("norm-1", "go test", 100)
	if !status.LoopDetected {
		t.Errorf("expected normalized actions to be detected as loop: %+v", status)
	}
}

func TestBudgetCheckMaxTokensZeroIgnores(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         100,
		MaxCostUSD:            1_000_000,
		MaxTokens:             0,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	status, err := b.Check("zero-1", "x", 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if status.Tripped {
		t.Error("MaxTokens=0 should disable token limit")
	}
}

func TestBudgetCheckPersistsAcrossCalls(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         100,
		MaxCostUSD:            100,
		MaxTokens:             1_000_000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	_, _ = b.Check("persist-2", "a", 100)
	_, _ = b.Check("persist-2", "b", 200)

	sess, ok := store.Get("persist-2")
	if !ok {
		t.Fatal("session not persisted")
	}
	if sess.TotalTokens != 300 {
		t.Errorf("tokens = %d, want 300", sess.TotalTokens)
	}
	if len(sess.RecentActions) != 2 {
		t.Errorf("actions = %d, want 2", len(sess.RecentActions))
	}
}

func TestBudgetCheckCustomCostFn(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	called := false
	b := NewBudget(Options{
		LoopThreshold: 100,
		MaxCostUSD:    100,
		MaxTokens:     1_000_000,
		CostFn: func(p, c, _ int64) float64 {
			called = true
			return float64(p+c) / 100.0
		},
		Store: store,
	})

	status, _ := b.Check("custom-1", "x", 500)
	if !called {
		t.Error("custom CostFn not invoked")
	}
	if status.TotalCostUSD != 5.0 {
		t.Errorf("expected custom cost 5.0, got %v", status.TotalCostUSD)
	}
}

func TestBudgetCheckDefaultCostFn(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         100,
		MaxCostUSD:            100,
		MaxTokens:             1_000_000,
		PromptPricePerMillion: 6.0,
		Store:                 store,
	})

	status, _ := b.Check("d-1", "x", 1_000_000)
	if status.TotalCostUSD != 6.0 {
		t.Errorf("default cost = %v, want 6.0", status.TotalCostUSD)
	}
}

func TestBudgetCheckActionRingBuffer(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         2,
		MaxCostUSD:            100,
		MaxTokens:             1_000_000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	for i := 0; i < 50; i++ {
		_, _ = b.Check("ring-1", "act", 10)
	}
	sess, _ := store.Get("ring-1")
	if len(sess.RecentActions) > 50 {
		t.Errorf("ring buffer overflowed: %d actions", len(sess.RecentActions))
	}
}

func resetSyncForTest(t *testing.T) {
	t.Helper()
	t.Setenv("GIST_CONFIG_DIR", t.TempDir())
}

func mustStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}