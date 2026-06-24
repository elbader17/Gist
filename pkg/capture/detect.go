package capture

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Prompt kinds reported in CaptureEvent.PromptKind.
const (
	KindJSON      = "json"
	KindCodeBlock = "code"
	KindMarkdown  = "markdown"
	KindLongText  = "long_text"
	KindOther     = "other"
)

// Minimum sizes to avoid classifying trivial output as a prompt.
const (
	MinPromptBytes = 32
	MinLongTextLen = 400
)

var (
	codeFenceRe = regexp.MustCompile("(?m)^```")
	headingRe   = regexp.MustCompile(`(?m)^#{1,6}\s+\S`)
	listRe      = regexp.MustCompile(`(?m)^\s*[-*+]\s+\S`)
	urlRe       = regexp.MustCompile(`https?://`)
)

// IsLikelyPrompt reports whether the given input looks like a prompt the
// user is sending to an LLM. The heuristic combines several signals:
//
//   - JSON payloads (OpenAI / Anthropic API requests).
//   - Markdown code fences or headings.
//   - Long plain-text blocks.
//
// Short, single-line input is never flagged.
func IsLikelyPrompt(s string) bool {
	if len(s) < MinPromptBytes {
		return false
	}
	if LooksLikeJSON(s) {
		return true
	}
	if codeFenceRe.MatchString(s) {
		return true
	}
	if headingRe.MatchString(s) {
		return true
	}
	if len(s) >= MinLongTextLen {
		return true
	}
	return false
}

// Classify returns the prompt kind for the given input.
func Classify(s string) string {
	switch {
	case LooksLikeJSON(s):
		return KindJSON
	case codeFenceRe.MatchString(s):
		return KindCodeBlock
	case headingRe.MatchString(s):
		return KindMarkdown
	case len(s) >= MinLongTextLen:
		return KindLongText
	default:
		return KindOther
	}
}

// LooksLikeJSON reports whether the trimmed input parses as JSON.
func LooksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	switch s[0] {
	case '{', '[':
	default:
		return false
	}
	var v interface{}
	return json.Unmarshal([]byte(s), &v) == nil
}

// ExtractLayers tries to split a captured prompt into (system, static) pairs
// that aligner.Align expects. The heuristic:
//
//   - If the input is JSON and contains "system" and "messages", use them.
//   - Otherwise, everything is treated as dynamic_input.
//
// The returned system and static slices are deduped and trimmed.
func ExtractLayers(s string) (system []string, static []string) {
	if LooksLikeJSON(s) {
		var payload struct {
			System   string   `json:"system"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			StaticFiles []string `json:"static_files"`
		}
		if err := json.Unmarshal([]byte(s), &payload); err == nil {
			if payload.System != "" {
				system = append(system, payload.System)
			}
			for _, m := range payload.Messages {
				if m.Role == "system" && m.Content != "" {
					system = append(system, m.Content)
				}
			}
			static = append(static, payload.StaticFiles...)
		}
	}
	if len(system) == 0 && len(static) == 0 {
		if urlRe.MatchString(s) {
			static = append(static, extractURLs(s)...)
		}
	}
	return dedupe(system), dedupe(static)
}

func extractURLs(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range urlRe.FindAllString(s, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}