// Package cache provides a thread-safe LRU cache for pruned file content.
//
// Entries are keyed by (path, mtime, size) so file updates invalidate
// automatically without explicit eviction. The cache is bounded by both
// the number of entries and the cumulative byte size of stored content.
//
// The primary consumer is pkg/squeeze which avoids re-parsing recently
// pruned files between consecutive calls.
package cache

import (
	"container/list"
	"os"
	"strconv"
	"sync"
)

// Entry is one cached pruned-file result.
type Entry struct {
	Path           string `json:"path"`
	MTime          int64  `json:"mtime"`
	Size           int64  `json:"size"`
	OriginalBytes  int64  `json:"original_bytes"`
	Slim           string `json:"slim"`
	SlimTokens     int    `json:"slim_tokens"`
	OriginalTokens int    `json:"original_tokens"`
}

// Cache is an LRU cache for Entries.
type Cache struct {
	mu         sync.Mutex
	maxEntries int
	maxBytes   int
	entries    map[string]*list.Element
	order      *list.List
	bytesTotal int
	hits       uint64
	misses     uint64
}

type lruItem struct {
	key   string
	entry *Entry
}

// New constructs a Cache. Defaults: 256 entries, 64 MB content.
func New(maxEntries, maxBytes int) *Cache {
	if maxEntries <= 0 {
		maxEntries = 256
	}
	if maxBytes <= 0 {
		maxBytes = 64 * 1024 * 1024
	}
	return &Cache{
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		entries:    make(map[string]*list.Element, maxEntries),
		order:      list.New(),
	}
}

// Get returns the cached entry for path, validating against the current
// mtime+size on disk. Returns (nil, false) on miss or stat error.
func (c *Cache) Get(path string) (*Entry, bool) {
	info, err := os.Stat(path)
	if err != nil {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, false
	}
	key := keyFor(path, info.ModTime().UnixNano(), info.Size())
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		c.hits++
		return el.Value.(*lruItem).entry, true
	}
	c.misses++
	return nil, false
}

// Put stores entry under the current disk mtime+size of path.
func (c *Cache) Put(path, slim string, slimTokens int) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	key := keyFor(path, info.ModTime().UnixNano(), info.Size())
	entry := &Entry{
		Path:           path,
		MTime:          info.ModTime().UnixNano(),
		Size:           info.Size(),
		OriginalBytes:  info.Size(),
		Slim:           slim,
		SlimTokens:     slimTokens,
		OriginalTokens: int(info.Size() / 4),
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		item := el.Value.(*lruItem)
		c.bytesTotal -= len(item.entry.Slim)
		item.entry = entry
		c.bytesTotal += len(slim)
		return
	}
	el := c.order.PushFront(&lruItem{key: key, entry: entry})
	c.entries[key] = el
	c.bytesTotal += len(slim)
	for c.order.Len() > c.maxEntries || c.bytesTotal > c.maxBytes {
		c.evictOldestLocked()
	}
}

// PutEntry stores an entry directly without stat-ing the file. Useful for
// pre-population from outside.
func (c *Cache) PutEntry(entry *Entry) {
	if entry == nil || entry.Path == "" {
		return
	}
	key := keyFor(entry.Path, entry.MTime, entry.Size)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		old := el.Value.(*lruItem).entry
		c.bytesTotal -= len(old.Slim)
		el.Value.(*lruItem).entry = entry
		c.bytesTotal += len(entry.Slim)
		return
	}
	el := c.order.PushFront(&lruItem{key: key, entry: entry})
	c.entries[key] = el
	c.bytesTotal += len(entry.Slim)
	for c.order.Len() > c.maxEntries || c.bytesTotal > c.maxBytes {
		c.evictOldestLocked()
	}
}

// Clear removes every entry.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*list.Element, c.maxEntries)
	c.order = list.New()
	c.bytesTotal = 0
}

// Stats returns current usage counters.
func (c *Cache) Stats() (entries, bytes int, hits, misses uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len(), c.bytesTotal, c.hits, c.misses
}

func (c *Cache) evictOldestLocked() {
	el := c.order.Back()
	if el == nil {
		return
	}
	item := el.Value.(*lruItem)
	delete(c.entries, item.key)
	c.order.Remove(el)
	c.bytesTotal -= len(item.entry.Slim)
}

func keyFor(path string, mtime, size int64) string {
	return path + "|" + strconv.FormatInt(mtime, 10) + "|" + strconv.FormatInt(size, 10)
}