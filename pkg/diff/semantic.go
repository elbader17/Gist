package diff

import (
	"bufio"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	goFuncRe     = regexp.MustCompile(`^func\s+(?:\([^)]+\)\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	goTypeRe     = regexp.MustCompile(`^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+(struct|interface)`)
	printlnRe    = regexp.MustCompile(`fmt\.Println|fmt\.Printf|console\.log|print\(`)
	commentRe    = regexp.MustCompile(`^\s*(//|\*|#)`)
	structuralRe = regexp.MustCompile(`^\s*(package\s+\w+|import\s|func\s.*\{$|func\s.*\)\s*\{$|type\s+\w+\s+(struct|interface)|\{|\}|\)\s*\{|\)\s*;?\s*$)`)
)

// extractFunctions returns the deduplicated list of Go function and type names
// that appear in added or removed lines of the patch.
func extractFunctions(patch string) []string {
	seen := make(map[string]bool)
	out := []string{}
	scanner := bufio.NewScanner(strings.NewReader(patch))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if !strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "-") {
			continue
		}
		body := strings.TrimPrefix(strings.TrimPrefix(line, "+"), "-")
		if m := goFuncRe.FindStringSubmatch(body); m != nil {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
			continue
		}
		if m := goTypeRe.FindStringSubmatch(body); m != nil {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1]+" (type)")
			}
		}
	}
	return out
}

// isLogOnly reports whether the patch contains only log/print additions
// (ignoring structural lines such as braces and function signatures).
func isLogOnly(patch string) bool {
	hasNonLog := false
	hasLog := false
	scanner := bufio.NewScanner(strings.NewReader(patch))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "+"))
		if body == "" || structuralRe.MatchString(body) {
			continue
		}
		if printlnRe.MatchString(body) {
			hasLog = true
		} else {
			hasNonLog = true
		}
	}
	return hasLog && !hasNonLog
}

// isCommentOnly reports whether the patch contains only comment additions.
func isCommentOnly(patch string) bool {
	hasNonComment := false
	hasComment := false
	scanner := bufio.NewScanner(strings.NewReader(patch))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "+"))
		if body == "" || structuralRe.MatchString(body) {
			continue
		}
		if commentRe.MatchString(body) {
			hasComment = true
		} else {
			hasNonComment = true
		}
	}
	return hasComment && !hasNonComment
}

func describe(fc *FileChange) string {
	name := filepath.Base(fc.Path)
	switch {
	case fc.LogOnly:
		return fmt.Sprintf("%s: only log/print additions", name)
	case fc.CommentOnly:
		return fmt.Sprintf("%s: only comment additions", name)
	case len(fc.Functions) > 0:
		return fmt.Sprintf("%s: modified %s", name, strings.Join(fc.Functions, ", "))
	default:
		return fmt.Sprintf("%s: +%d -%d lines", name, fc.AddedLines, fc.RemovedLines)
	}
}