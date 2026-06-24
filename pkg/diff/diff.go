// Package diff implements semantic git diff summarization for the
// fetch_diff_context tool.
//
// The package runs git as a subprocess and parses its output. Two phases:
//
//  1. Fetch runs `git diff --numstat` to get a fast per-file line count.
//  2. Enrich re-runs `git diff --unified=0 <base> -- <path>` per file to
//     extract modified function/type names and detect log-only or
//     comment-only changes.
package diff

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// FileChange describes a single modified file in the diff.
type FileChange struct {
	Path         string   `json:"path"`
	Status       string   `json:"status"`
	AddedLines   int      `json:"added_lines"`
	RemovedLines int      `json:"removed_lines"`
	Functions    []string `json:"functions,omitempty"`
	Summary      string   `json:"summary"`
	LogOnly      bool     `json:"log_only"`
	CommentOnly  bool     `json:"comment_only"`
}

// SemanticDiff is the full result of a Fetch + Enrich cycle.
type SemanticDiff struct {
	Target       string        `json:"target"`
	Base         string        `json:"base"`
	Files        []*FileChange `json:"files"`
	Summary      string        `json:"summary"`
	TotalAdded   int           `json:"total_added"`
	TotalRemoved int           `json:"total_removed"`
	Truncated    bool          `json:"truncated,omitempty"`
}

// Options configures Fetch.
type Options struct {
	TargetBranch string
	Base         string
	Cwd          string
	MaxFiles     int
}

// Fetch runs git and returns the file-level diff summary. Pair with Enrich
// to populate function names and log/comment flags per file.
func Fetch(opts Options) (*SemanticDiff, error) {
	if opts.Base == "" {
		opts.Base = "HEAD"
	}
	cwd := opts.Cwd
	if cwd == "" {
		cwd = "."
	}

	stat, err := gitDiff(cwd, opts.Base, opts.TargetBranch, "--numstat", "-M", "--no-color")
	if err != nil {
		return nil, fmt.Errorf("git stat: %w", err)
	}

	files := parseNumStat(stat)
	if opts.MaxFiles > 0 && len(files) > opts.MaxFiles {
		files = files[:opts.MaxFiles]
	}

	semantic := &SemanticDiff{
		Target: opts.TargetBranch,
		Base:   opts.Base,
		Files:  files,
	}

	totalAdded := 0
	totalRemoved := 0
	allLog := true
	allComment := true

	for _, fc := range files {
		totalAdded += fc.AddedLines
		totalRemoved += fc.RemovedLines
		if !fc.LogOnly {
			allLog = false
		}
		if !fc.CommentOnly {
			allComment = false
		}
		if fc.Functions == nil {
			fc.Functions = []string{}
		}
	}

	semantic.TotalAdded = totalAdded
	semantic.TotalRemoved = totalRemoved

	switch {
	case allLog && totalAdded > 0:
		semantic.Summary = fmt.Sprintf("Only fmt.Println/log additions across %d files (+%d -%d)", len(files), totalAdded, totalRemoved)
	case allComment && totalAdded > 0:
		semantic.Summary = fmt.Sprintf("Only comment additions across %d files (+%d -%d)", len(files), totalAdded, totalRemoved)
	default:
		semantic.Summary = fmt.Sprintf("%d files changed, +%d -%d lines", len(files), totalAdded, totalRemoved)
	}

	return semantic, nil
}

// Enrich walks files in-place and populates Functions, LogOnly, CommentOnly,
// and Summary by parsing per-file diffs.
func Enrich(cwd string, base string, files []*FileChange) error {
	if base == "" {
		base = "HEAD"
	}
	for _, fc := range files {
		if fc == nil || fc.Path == "" {
			continue
		}
		patch, err := gitDiffFile(cwd, base, fc.Path)
		if err != nil {
			continue
		}
		fc.Functions = extractFunctions(patch)
		fc.LogOnly = isLogOnly(patch)
		fc.CommentOnly = isCommentOnly(patch)
		fc.Summary = describe(fc)
	}
	return nil
}

// GitIsRepo reports whether cwd is inside a git working tree.
func GitIsRepo(cwd string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = cwd
	return cmd.Run() == nil
}

func gitDiff(cwd, base, target string, extraArgs ...string) (string, error) {
	args := []string{"diff", "--no-color"}
	args = append(args, extraArgs...)

	if target != "" && target != "HEAD" {
		args = append(args, base+"..."+target)
	} else if base != "" {
		args = append(args, base)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func gitDiffFile(cwd, base, path string) (string, error) {
	args := []string{"diff", "--no-color", "--unified=0"}
	if base != "" {
		args = append(args, base)
	}
	args = append(args, "--", path)
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseStat(out string) []*FileChange {
	return parseNumStat(out)
}

func parseNumStat(out string) []*FileChange {
	if out == "" {
		return nil
	}
	files := []*FileChange{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		if parts[0] == "-" || parts[1] == "-" {
			continue
		}
		path := strings.TrimSpace(parts[2])
		if path == "" {
			continue
		}
		added := parseNum(parts[0])
		removed := parseNum(parts[1])
		files = append(files, &FileChange{
			Path:         filepath.ToSlash(path),
			AddedLines:   added,
			RemovedLines: removed,
			Status:       classify(path),
			Summary:      fmt.Sprintf("+%d -%d lines", added, removed),
			Functions:    []string{},
		})
	}
	return files
}

func parseNum(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	started := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
			started = true
		} else if started {
			break
		}
	}
	return n
}

func classify(path string) string {
	if strings.HasPrefix(path, "R") || strings.HasPrefix(path, "M") {
		return "renamed"
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return "modified"
	}
	return "modified"
}