// Package metrics tracks per-session token savings across MCP tool calls.
//
// Every call to a Gist MCP tool can record an observation describing the
// number of input tokens (raw content the client would have sent) versus
// the output tokens (what Gist actually returned). The aggregate is
// surfaced through the report_savings MCP tool and persisted to disk via a
// background flusher that runs every two seconds.
package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Observation is a single recorded token-saving event.
type Observation struct {
	SessionID    string    `json:"session_id"`
	Tool         string    `json:"tool"`
	Timestamp    time.Time `json:"timestamp"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	SavedTokens  int       `json:"saved_tokens"`
}

// ToolMetrics aggregates counts for one tool within a session or globally.
type ToolMetrics struct {
	Tool         string `json:"tool"`
	CallCount    int    `json:"call_count"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	SavedTokens  int    `json:"saved_tokens"`
}

// SessionMetrics aggregates counts for a single session.
type SessionMetrics struct {
	SessionID    string                    `json:"session_id"`
	Observations []Observation             `json:"observations,omitempty"`
	InputTokens  int                       `json:"input_tokens"`
	OutputTokens int                       `json:"output_tokens"`
	SavedTokens  int                       `json:"saved_tokens"`
	CallCount    int                       `json:"call_count"`
	ByTool       map[string]*ToolMetrics   `json:"by_tool,omitempty"`
}

// Snapshot is the on-disk representation of a Recorder.
type Snapshot struct {
	Sessions map[string]*SessionMetrics `json:"sessions"`
}

// Aggregate summarizes savings across every session.
type Aggregate struct {
	Sessions     int                     `json:"sessions"`
	CallCount    int                     `json:"call_count"`
	InputTokens  int                     `json:"input_tokens"`
	OutputTokens int                     `json:"output_tokens"`
	SavedTokens  int                     `json:"saved_tokens"`
	SavedRatio   float64                 `json:"saved_ratio"`
	ByTool       map[string]*ToolMetrics `json:"by_tool"`
}

// Recorder is the central sink for token-saving observations.
type Recorder struct {
	mu       sync.Mutex
	sessions map[string]*SessionMetrics
	path     string

	stopCh   chan struct{}
	stopOnce sync.Once
	doneCh   chan struct{}
}

// NewRecorder constructs a Recorder backed by path. It loads existing
// data synchronously and starts a background flusher that writes the
// snapshot to disk every two seconds. Call Stop to terminate the flusher.
func NewRecorder(path string) *Recorder {
	r := &Recorder{
		sessions: make(map[string]*SessionMetrics),
		path:     path,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	r.load()
	go r.flusher()
	return r
}

// Record adds an observation for (sessionID, tool) with the given
// token counts. Negative savings are clamped to zero.
func (r *Recorder) Record(sessionID, tool string, inputTokens, outputTokens int) {
	if sessionID == "" || tool == "" {
		return
	}
	saved := inputTokens - outputTokens
	if saved < 0 {
		saved = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.sessions[sessionID]
	if !ok {
		sess = &SessionMetrics{
			SessionID:    sessionID,
			Observations: make([]Observation, 0, 16),
			ByTool:       make(map[string]*ToolMetrics),
		}
		r.sessions[sessionID] = sess
	}
	sess.Observations = append(sess.Observations, Observation{
		SessionID:    sessionID,
		Tool:         tool,
		Timestamp:    time.Now().UTC(),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		SavedTokens:  saved,
	})
	sess.InputTokens += inputTokens
	sess.OutputTokens += outputTokens
	sess.SavedTokens += saved
	sess.CallCount++
	tm, ok := sess.ByTool[tool]
	if !ok {
		tm = &ToolMetrics{Tool: tool}
		sess.ByTool[tool] = tm
	}
	tm.CallCount++
	tm.InputTokens += inputTokens
	tm.OutputTokens += outputTokens
	tm.SavedTokens += saved
}

// Snapshot returns a deep copy of the current state, safe to marshal.
func (r *Recorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := Snapshot{Sessions: make(map[string]*SessionMetrics, len(r.sessions))}
	for k, v := range r.sessions {
		clone := *v
		clone.Observations = append([]Observation(nil), v.Observations...)
		clone.ByTool = make(map[string]*ToolMetrics, len(v.ByTool))
		for tk, tv := range v.ByTool {
			tc := *tv
			clone.ByTool[tk] = &tc
		}
		out.Sessions[k] = &clone
	}
	return out
}

// Aggregate returns a global summary across every recorded session.
func (r *Recorder) Aggregate() Aggregate {
	r.mu.Lock()
	defer r.mu.Unlock()
	agg := Aggregate{ByTool: make(map[string]*ToolMetrics)}
	for _, sess := range r.sessions {
		agg.Sessions++
		agg.InputTokens += sess.InputTokens
		agg.OutputTokens += sess.OutputTokens
		agg.SavedTokens += sess.SavedTokens
		agg.CallCount += sess.CallCount
		for tk, tm := range sess.ByTool {
			at, ok := agg.ByTool[tk]
			if !ok {
				at = &ToolMetrics{Tool: tk}
				agg.ByTool[tk] = at
			}
			at.CallCount += tm.CallCount
			at.InputTokens += tm.InputTokens
			at.OutputTokens += tm.OutputTokens
			at.SavedTokens += tm.SavedTokens
		}
	}
	if agg.InputTokens > 0 {
		agg.SavedRatio = float64(agg.SavedTokens) / float64(agg.InputTokens)
	}
	return agg
}

// Session returns the metrics for sessionID, or false when absent.
func (r *Recorder) Session(sessionID string) (SessionMetrics, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.sessions[sessionID]
	if !ok {
		return SessionMetrics{}, false
	}
	clone := *sess
	clone.Observations = append([]Observation(nil), sess.Observations...)
	clone.ByTool = make(map[string]*ToolMetrics, len(sess.ByTool))
	for tk, tv := range sess.ByTool {
		tc := *tv
		clone.ByTool[tk] = &tc
	}
	return clone, true
}

// Stop halts the background flusher and performs a final synchronous
// flush. Safe to call multiple times.
func (r *Recorder) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })
	<-r.doneCh
}

func (r *Recorder) flusher() {
	defer close(r.doneCh)
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.stopCh:
			_ = r.save()
			return
		case <-t.C:
			_ = r.save()
		}
	}
}

func (r *Recorder) save() error {
	if r.path == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.persistLocked()
}

func (r *Recorder) persistLocked() error {
	if r.path == "" {
		return nil
	}
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	snap := Snapshot{Sessions: r.sessions}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *Recorder) load() {
	if r.path == "" {
		return
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil || snap.Sessions == nil {
		return
	}
	r.sessions = snap.Sessions
	for k, v := range snap.Sessions {
		if v.ByTool == nil {
			v.ByTool = make(map[string]*ToolMetrics)
		}
		if v.Observations == nil {
			v.Observations = []Observation{}
		}
		r.sessions[k] = v
	}
}