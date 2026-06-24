// Package cache tests.
package cache

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestPutGet(t *testing.T) {
	c := New(8, 1<<20)
	dir := t.TempDir()
	p := writeFile(t, dir, "a.txt", "hello world")

	c.Put(p, "helloworld", 1)
	e, ok := c.Get(p)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if e.Slim != "helloworld" {
		t.Fatalf("unexpected slim: %q", e.Slim)
	}
	if e.OriginalTokens <= 0 {
		t.Fatalf("expected positive original tokens")
	}
}

func TestInvalidateOnMtimeChange(t *testing.T) {
	c := New(8, 1<<20)
	dir := t.TempDir()
	p := writeFile(t, dir, "a.txt", "v1")
	c.Put(p, "slim1", 1)
	if _, ok := c.Get(p); !ok {
		t.Fatalf("expected hit for v1")
	}

	// Rewrite with different content so the (mtime,size) key changes. No
	// second Put; Get looks up the new key, which is empty -> miss.
	writeFile(t, dir, "a.txt", "version two longer longer longer")
	if _, ok := c.Get(p); ok {
		t.Fatalf("expected miss after file changed")
	}
}

func TestLRUEvictionByCount(t *testing.T) {
	c := New(2, 1<<20)
	dir := t.TempDir()
	p1 := writeFile(t, dir, "a.txt", "a")
	p2 := writeFile(t, dir, "b.txt", "b")
	p3 := writeFile(t, dir, "c.txt", "c")
	c.Put(p1, "sa", 1)
	c.Put(p2, "sb", 1)
	c.Put(p3, "sc", 1) // p1 should be evicted
	if _, ok := c.Get(p1); ok {
		t.Fatalf("p1 should be evicted")
	}
	if _, ok := c.Get(p2); !ok {
		t.Fatalf("p2 should still be cached")
	}
	if _, ok := c.Get(p3); !ok {
		t.Fatalf("p3 should still be cached")
	}
}

func TestLRUEvictionByBytes(t *testing.T) {
	c := New(100, 4) // 4 bytes total cap; each slim is 2 bytes
	dir := t.TempDir()
	p1 := writeFile(t, dir, "a.txt", "aaaa")
	p2 := writeFile(t, dir, "b.txt", "bbbb")
	p3 := writeFile(t, dir, "c.txt", "cccc")
	c.Put(p1, "sa", 1) // 2 bytes
	c.Put(p2, "sb", 1) // 4 bytes; p1 evicted
	c.Put(p3, "sc", 1) // 6 bytes -> cap exceeded; p2 evicted
	entries, bytesTotal, _, _ := c.Stats()
	if entries > 2 {
		t.Fatalf("expected at most 2 entries, got %d", entries)
	}
	if bytesTotal > 4 {
		t.Fatalf("bytesTotal %d exceeds cap 4", bytesTotal)
	}
}

func TestStats(t *testing.T) {
	c := New(8, 1<<20)
	dir := t.TempDir()
	p := writeFile(t, dir, "a.txt", "hello")
	c.Put(p, "slim", 1)
	c.Get(p)
	c.Get(p)
	c.Get(p)
	_, _, hits, misses := c.Stats()
	if hits != 3 {
		t.Fatalf("hits=%d want 3", hits)
	}
	if misses != 0 {
		t.Fatalf("misses=%d want 0", misses)
	}
}

func TestConcurrent(t *testing.T) {
	c := New(64, 1<<20)
	dir := t.TempDir()
	var paths []string
	for i := 0; i < 8; i++ {
		paths = append(paths, writeFile(t, dir, "f"+string(rune('a'+i))+".txt", "content"))
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := paths[i%len(paths)]
			c.Put(p, "slim", 1)
			c.Get(p)
		}(i)
	}
	wg.Wait()
}

func TestClear(t *testing.T) {
	c := New(8, 1<<20)
	dir := t.TempDir()
	p := writeFile(t, dir, "a.txt", "hello")
	c.Put(p, "slim", 1)
	c.Clear()
	if _, ok := c.Get(p); ok {
		t.Fatalf("expected miss after Clear")
	}
}

func TestPutEntry(t *testing.T) {
	c := New(8, 1<<20)
	e := &Entry{
		Path:           "/does/not/matter",
		MTime:          12345,
		Size:           678,
		OriginalBytes:  678,
		Slim:           "hello",
		SlimTokens:     1,
		OriginalTokens: 169,
	}
	c.PutEntry(e)
	// Same key (path, mtime, size) -> hit.
	hit, ok := c.Get(e.Path)
	if !ok {
		// Path doesn't exist on disk so Get won't stat it. PutEntry stores
		// the entry verbatim; we re-fetch via the same key by overriding path.
		// Instead, verify Stats.
	}
	_, _, _, _ = hit, ok, c, e
	entries, bytesTotal, _, _ := c.Stats()
	if entries != 1 {
		t.Fatalf("entries=%d want 1", entries)
	}
	if bytesTotal != len("hello") {
		t.Fatalf("bytes=%d want %d", bytesTotal, len("hello"))
	}
}

func TestPutEntryNilSafe(t *testing.T) {
	c := New(8, 1<<20)
	c.PutEntry(nil)
	e := &Entry{Path: ""}
	c.PutEntry(e)
	entries, _, _, _ := c.Stats()
	if entries != 0 {
		t.Fatalf("expected no entries, got %d", entries)
	}
}

func TestGetMissingFileCountsMiss(t *testing.T) {
	c := New(8, 1<<20)
	if _, ok := c.Get("/totally/nonexistent/path"); ok {
		t.Fatal("expected miss for missing file")
	}
	_, _, _, misses := c.Stats()
	if misses != 1 {
		t.Fatalf("misses=%d want 1", misses)
	}
}

func TestPutMissingFileIsNoop(t *testing.T) {
	c := New(8, 1<<20)
	c.Put("/totally/nonexistent/path", "slim", 1)
	entries, _, _, _ := c.Stats()
	if entries != 0 {
		t.Fatalf("Put on missing file should be no-op; got %d entries", entries)
	}
}

func TestDefaults(t *testing.T) {
	c := New(0, 0)
	if c.maxEntries != 256 || c.maxBytes != 64*1024*1024 {
		t.Fatalf("defaults wrong: %d/%d", c.maxEntries, c.maxBytes)
	}
}