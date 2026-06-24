// Package budget implements the per-session circuit breaker.
//
// Each session tracks cumulative tokens, cost, and a sliding window of
// recent actions. The Budget type evaluates incoming actions against three
// trip conditions:
//
//   - Loop detection: same action repeated N times consecutively.
//   - Cost ceiling:   cumulative cost >= max_cost_usd.
//   - Token ceiling:  cumulative tokens >= max_tokens.
//
// When tripped, the returned BudgetStatus has Allowed=false and the MCP
// dispatcher surfaces this as an isError response so the agent must seek
// human authorization before continuing.
package budget

import (
	"fmt"
	"strings"
	"time"
)

// BudgetStatus is the result of a Budget.Check call.
type BudgetStatus struct {
	Allowed          bool    `json:"allowed"`
	Tripped          bool    `json:"tripped"`
	Reason           string  `json:"reason,omitempty"`
	TotalTokens      int64   `json:"total_tokens"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	RemainingUSD     float64 `json:"remaining_usd"`
	MaxCostUSD       float64 `json:"max_cost_usd"`
	MaxTokens        int64   `json:"max_tokens"`
	LoopDetected     bool    `json:"loop_detected"`
	RepeatedAction   string  `json:"repeated_action,omitempty"`
	RepeatedCount    int     `json:"repeated_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`
}

// Budget is the in-memory policy + counter for a process.
type Budget struct {
	loopThreshold    int
	maxCostUSD       float64
	maxTokens        int64
	costForTokensFn  func(prompt, completion, cached int64) float64
	promptCostPerTok float64
	store            *Store
	flusher          *Flusher
}

// Options configures a new Budget.
type Options struct {
	LoopThreshold         int
	MaxCostUSD            float64
	MaxTokens             int64
	PromptPricePerMillion float64
	CostFn                func(prompt, completion, cached int64) float64
	Store                 *Store
	Flusher               *Flusher
}

// NewBudget creates a Budget. CostFn defaults to
// prompt*PromptPricePerMillion + completion*15.0 when nil.
func NewBudget(opts Options) *Budget {
	if opts.LoopThreshold <= 0 {
		opts.LoopThreshold = 3
	}
	costFn := opts.CostFn
	if costFn == nil {
		costFn = func(p, c, _ int64) float64 {
			prompt := float64(p) / 1_000_000.0
			completion := float64(c) / 1_000_000.0
			return prompt*opts.PromptPricePerMillion + completion*15.0
		}
	}
	return &Budget{
		loopThreshold:    opts.LoopThreshold,
		maxCostUSD:       opts.MaxCostUSD,
		maxTokens:        opts.MaxTokens,
		costForTokensFn:  costFn,
		promptCostPerTok: opts.PromptPricePerMillion / 1_000_000.0,
		store:            opts.Store,
		flusher:          opts.Flusher,
	}
}

// Check records an action and returns whether the session is allowed to
// proceed. sessionID identifies the agent's session; the same id must be used
// across all calls within a session for accurate accounting.
func (b *Budget) Check(sessionID, currentAction string, estimatedTokens int64) (*BudgetStatus, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if b.store == nil {
		return nil, fmt.Errorf("budget store not configured")
	}
	sess := b.store.GetOrCreate(sessionID)

	sess.Lock()
	defer sess.Unlock()

	sess.UpdatedAt = time.Now().UTC()
	sess.TotalTokens += estimatedTokens
	sess.PromptTokens += estimatedTokens
	sess.TotalCostUSD = b.costForTokensFn(sess.PromptTokens, sess.CompletionTokens, sess.CachedTokens)

	sess.RecentActions = append(sess.RecentActions, Action{
		Timestamp: sess.UpdatedAt,
		Action:    currentAction,
		Tokens:    estimatedTokens,
		Cost:      float64(estimatedTokens) * b.promptCostPerTok,
	})

	maxActions := b.loopThreshold * 4
	if maxActions < 8 {
		maxActions = 8
	}
	if len(sess.RecentActions) > maxActions {
		sess.RecentActions = sess.RecentActions[len(sess.RecentActions)-maxActions:]
	}

	status := &BudgetStatus{
		TotalTokens:      sess.TotalTokens,
		TotalCostUSD:     sess.TotalCostUSD,
		RemainingUSD:     b.maxCostUSD - sess.TotalCostUSD,
		MaxCostUSD:       b.maxCostUSD,
		MaxTokens:        b.maxTokens,
		PromptTokens:     sess.PromptTokens,
		CompletionTokens: sess.CompletionTokens,
		CachedTokens:     sess.CachedTokens,
		Allowed:          true,
	}

	loopAction, loopCount := detectLoop(sess.RecentActions, currentAction)
	if loopCount >= b.loopThreshold {
		status.LoopDetected = true
		status.RepeatedAction = loopAction
		status.RepeatedCount = loopCount
		status.Allowed = false
		status.Tripped = true
		status.Reason = fmt.Sprintf(
			"Loop detected: action %q repeated %d times (threshold %d)",
			loopAction, loopCount, b.loopThreshold,
		)
		sess.Tripped = true
		sess.TripCount++
		b.persist(true)
		return status, nil
	}

	if b.maxCostUSD > 0 && sess.TotalCostUSD >= b.maxCostUSD {
		status.Allowed = false
		status.Tripped = true
		status.Reason = fmt.Sprintf(
			"Session cost limit reached: $%.4f >= $%.2f",
			sess.TotalCostUSD, b.maxCostUSD,
		)
		sess.Tripped = true
		sess.TripCount++
		b.persist(true)
		return status, nil
	}

	if b.maxTokens > 0 && sess.TotalTokens >= b.maxTokens {
		status.Allowed = false
		status.Tripped = true
		status.Reason = fmt.Sprintf(
			"Session token limit reached: %d >= %d",
			sess.TotalTokens, b.maxTokens,
		)
		sess.Tripped = true
		sess.TripCount++
		b.persist(true)
		return status, nil
	}

	b.persist(false)
	return status, nil
}

// persist records the mutation. When force is true (trip conditions) the
// store flushes immediately; otherwise the optional flusher debounces.
func (b *Budget) persist(force bool) {
	if b.store == nil {
		return
	}
	if force {
		_ = b.store.Save()
		return
	}
	if b.flusher != nil {
		b.flusher.Mark()
		return
	}
	_ = b.store.Save()
}

func detectLoop(actions []Action, current string) (string, int) {
	current = normalize(current)
	if current == "" {
		return "", 0
	}
	count := 1
	var lastMatch string
	for i := len(actions) - 2; i >= 0; i-- {
		if normalize(actions[i].Action) == current {
			count++
			lastMatch = actions[i].Action
		} else if count > 1 {
			break
		}
	}
	return lastMatch, count
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}