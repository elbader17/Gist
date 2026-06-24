package budget

import (
	"sync"
	"time"
)

// Flusher debounces store saves to avoid hitting disk on every Budget.Check.
// It flushes at most once per Interval, plus an immediate flush on trip.
type Flusher struct {
	store    *Store
	interval time.Duration
	dirty    bool

	mu       sync.Mutex
	stopCh   chan struct{}
	stopOnce sync.Once
	doneCh   chan struct{}
}

// NewFlusher wraps store with a periodic saver. If interval <= 0, defaults
// to 2 seconds. The flusher runs until Stop is called.
func NewFlusher(store *Store, interval time.Duration) *Flusher {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	f := &Flusher{
		store:    store,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go f.loop()
	return f
}

func (f *Flusher) loop() {
	defer close(f.doneCh)
	t := time.NewTicker(f.interval)
	defer t.Stop()
	for {
		select {
		case <-f.stopCh:
			f.flush()
			return
		case <-t.C:
			f.flush()
		}
	}
}

// Mark records that the store was mutated. The next ticker tick (or
// Stop) will persist it.
func (f *Flusher) Mark() {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.dirty = true
	f.mu.Unlock()
}

// FlushNow persists immediately if dirty.
func (f *Flusher) FlushNow() error {
	if f == nil {
		return nil
	}
	return f.flush()
}

func (f *Flusher) flush() error {
	if f == nil || f.store == nil {
		return nil
	}
	f.mu.Lock()
	dirty := f.dirty
	f.dirty = false
	f.mu.Unlock()
	if !dirty {
		return nil
	}
	return f.store.Save()
}

// Stop terminates the background loop and forces a final flush.
func (f *Flusher) Stop() error {
	if f == nil {
		return nil
	}
	f.stopOnce.Do(func() { close(f.stopCh) })
	<-f.doneCh
	return nil
}