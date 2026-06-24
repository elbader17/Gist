package diff

import (
	"sync"
)

const defaultParallelism = 8

// EnrichParallel walks files concurrently, populating Functions,
// LogOnly, CommentOnly, and Summary for each. It uses up to maxWorkers
// goroutines (capped at defaultParallelism). The original Enrich is kept
// for callers that need strict serialization.
func EnrichParallel(cwd, base string, files []*FileChange, maxWorkers int) error {
	if base == "" {
		base = "HEAD"
	}
	if maxWorkers <= 0 {
		maxWorkers = defaultParallelism
	}
	if maxWorkers > defaultParallelism {
		maxWorkers = defaultParallelism
	}
	if maxWorkers < 1 {
		maxWorkers = 1
	}

	work := make([]*FileChange, 0, len(files))
	for _, fc := range files {
		if fc == nil || fc.Path == "" {
			continue
		}
		work = append(work, fc)
	}
	if len(work) == 0 {
		return nil
	}

	jobs := make(chan *FileChange, len(work))
	for _, fc := range work {
		jobs <- fc
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fc := range jobs {
				patch, err := gitDiffFile(cwd, base, fc.Path)
				if err != nil {
					continue
				}
				fc.Functions = extractFunctions(patch)
				fc.LogOnly = isLogOnly(patch)
				fc.CommentOnly = isCommentOnly(patch)
				fc.Summary = describe(fc)
			}
		}()
	}
	wg.Wait()
	return nil
}