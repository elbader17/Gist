package budget

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GIST_CONFIG_DIR", dir)
	return &Store{
		path:     filepath.Join(dir, "sessions.json"),
		Sessions: make(map[string]*Session),
	}
}

func TestFlusherDebounces(t *testing.T) {
	store := setupStore(t)
	f := NewFlusher(store, 10*time.Millisecond)
	defer f.Stop()

	f.Mark()
	f.Mark()
	f.Mark()

	// Disk should be empty right away.
	if _, err := os.Stat(store.path); !os.IsNotExist(err) {
		t.Fatalf("expected no file yet, got err=%v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("expected file after flush window: %v", err)
	}
}

func TestFlusherStopFlushesImmediately(t *testing.T) {
	store := setupStore(t)
	f := NewFlusher(store, 1*time.Hour)
	f.Mark()
	if err := f.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("expected immediate flush on stop, got %v", err)
	}
}

func TestFlusherFlushNowSkipsWhenClean(t *testing.T) {
	store := setupStore(t)
	f := NewFlusher(store, 1*time.Hour)
	defer f.Stop()
	// Not marked dirty; should be a no-op.
	if err := f.FlushNow(); err != nil {
		t.Fatalf("flushnow: %v", err)
	}
	if _, err := os.Stat(store.path); !os.IsNotExist(err) {
		t.Fatalf("expected no file when clean")
	}
}

func TestBudgetPersistsViaFlusher(t *testing.T) {
	store := setupStore(t)
	f := NewFlusher(store, 20*time.Millisecond)
	b := NewBudget(Options{
		LoopThreshold:         3,
		MaxCostUSD:            100,
		PromptPricePerMillion: 3,
		Store:                 store,
		Flusher:               f,
	})
	defer f.Stop()

	if _, err := b.Check("s1", "go test", 100); err != nil {
		t.Fatalf("check: %v", err)
	}
	// Disk should not have been written immediately.
	if _, err := os.Stat(store.path); !os.IsNotExist(err) {
		t.Fatalf("expected debounced write; file exists already")
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("expected eventual flush: %v", err)
	}
}

func TestBudgetForcesImmediateFlushOnTrip(t *testing.T) {
	store := setupStore(t)
	f := NewFlusher(store, 1*time.Hour)
	defer f.Stop()
	b := NewBudget(Options{
		LoopThreshold:         2,
		MaxCostUSD:            100,
		PromptPricePerMillion: 3,
		Store:                 store,
		Flusher:               f,
	})

	if _, err := b.Check("s1", "go test", 10); err != nil {
		t.Fatalf("check1: %v", err)
	}
	if _, err := b.Check("s1", "go test", 10); err != nil {
		t.Fatalf("check2: %v", err)
	}
	// Third call trips loop; should force immediate flush.
	if _, err := b.Check("s1", "go test", 10); err != nil {
		t.Fatalf("check3: %v", err)
	}
	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("trip should force flush, got %v", err)
	}
}