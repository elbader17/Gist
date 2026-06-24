package budget

import (
	"sync"
	"testing"
)

func TestBudgetConcurrentSafety(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         1000,
		MaxCostUSD:            10000,
		MaxTokens:             1_000_000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	var wg sync.WaitGroup
	const goroutines = 20
	const iterations = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = b.Check("concurrent-1", "action", 10)
			}
		}(i)
	}
	wg.Wait()

	sess, _ := store.Get("concurrent-1")
	if sess.TotalTokens != int64(goroutines*iterations*10) {
		t.Errorf("tokens = %d, want %d", sess.TotalTokens, goroutines*iterations*10)
	}
}

func TestBudgetIndependentSessions(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	b := NewBudget(Options{
		LoopThreshold:         3,
		MaxCostUSD:            100,
		MaxTokens:             1_000_000,
		PromptPricePerMillion: 3.0,
		Store:                 store,
	})

	_, _ = b.Check("a", "x", 100)
	_, _ = b.Check("b", "y", 100)

	sessA, _ := store.Get("a")
	sessB, _ := store.Get("b")
	if sessA.TotalTokens != 100 || sessB.TotalTokens != 100 {
		t.Errorf("independent sessions should not share tokens")
	}
}

func TestSessionLockUnlock(t *testing.T) {
	resetSyncForTest(t)
	store := mustStore(t)
	s := store.GetOrCreate("lock-1")
	s.Lock()
	s.TotalTokens = 50
	s.Unlock()
	if s.TotalTokens != 50 {
		t.Error("Lock/Unlock should not corrupt state")
	}
}