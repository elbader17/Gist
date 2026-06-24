// Package metrics tests.
package metrics

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRecordAndAggregate(t *testing.T) {
	r := NewRecorder("")
	defer r.Stop()

	r.Record("s1", "view_file_slim", 1000, 200)
	r.Record("s1", "view_file_slim", 500, 100)
	r.Record("s1", "squeeze_context", 8000, 2000)

	agg := r.Aggregate()
	if agg.Sessions != 1 {
		t.Fatalf("sessions=%d want 1", agg.Sessions)
	}
	if agg.InputTokens != 9500 {
		t.Fatalf("input=%d want 9500", agg.InputTokens)
	}
	if agg.OutputTokens != 2300 {
		t.Fatalf("output=%d want 2300", agg.OutputTokens)
	}
	if agg.SavedTokens != 7200 {
		t.Fatalf("saved=%d want 7200", agg.SavedTokens)
	}
	if agg.CallCount != 3 {
		t.Fatalf("calls=%d want 3", agg.CallCount)
	}
	if agg.ByTool["view_file_slim"].CallCount != 2 {
		t.Fatalf("view_file_slim calls=%d want 2", agg.ByTool["view_file_slim"].CallCount)
	}
	if agg.ByTool["squeeze_context"].CallCount != 1 {
		t.Fatalf("squeeze_context calls=%d want 1", agg.ByTool["squeeze_context"].CallCount)
	}
	wantRatio := float64(7200) / float64(9500)
	if diff := agg.SavedRatio - wantRatio; diff > 0.0001 || diff < -0.0001 {
		t.Fatalf("ratio=%.4f want %.4f", agg.SavedRatio, wantRatio)
	}
}

func TestSessionLookup(t *testing.T) {
	r := NewRecorder("")
	defer r.Stop()
	r.Record("alpha", "view_file_slim", 100, 20)
	s, ok := r.Session("alpha")
	if !ok {
		t.Fatalf("expected session")
	}
	if s.InputTokens != 100 || s.OutputTokens != 20 || s.SavedTokens != 80 {
		t.Fatalf("unexpected metrics: %+v", s)
	}
	if _, ok := r.Session("missing"); ok {
		t.Fatalf("did not expect missing session")
	}
}

func TestNegativeSavedClampedToZero(t *testing.T) {
	r := NewRecorder("")
	defer r.Stop()
	r.Record("s", "tool", 10, 50) // negative saved
	agg := r.Aggregate()
	if agg.SavedTokens != 0 {
		t.Fatalf("expected clamped zero, got %d", agg.SavedTokens)
	}
	if agg.OutputTokens != 50 {
		t.Fatalf("output should still count: %d", agg.OutputTokens)
	}
}

func TestSnapshotDeepCopy(t *testing.T) {
	r := NewRecorder("")
	defer r.Stop()
	r.Record("s", "tool", 100, 10)
	snap := r.Snapshot()
	snap.Sessions["s"].InputTokens = 999
	// Mutating the snapshot must not affect the recorder.
	s2, _ := r.Session("s")
	if s2.InputTokens != 100 {
		t.Fatalf("recorder state was mutated by snapshot edit")
	}
}

func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	r1 := NewRecorder(path)
	r1.Record("s", "view_file_slim", 200, 50)
	// Wait briefly for the flusher (ticker is 2s; we just call Save explicitly).
	if err := r1.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	r1.Stop()

	r2 := NewRecorder(path)
	defer r2.Stop()
	if _, ok := r2.Session("s"); !ok {
		t.Fatalf("expected session to be persisted")
	}
	agg := r2.Aggregate()
	if agg.SavedTokens != 150 {
		t.Fatalf("persisted saved=%d want 150", agg.SavedTokens)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	r := NewRecorder("")
	r.Stop()
	time.Sleep(10 * time.Millisecond)
	r.Stop() // must not panic
}