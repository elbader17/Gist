// Package tokenizer provides approximate BPE-style token counting.
//
// The implementation uses fixed byte-to-token ratios per encoding:
//
//	cl100k_base  ~4.0 bytes/token
//	o200k_base   ~3.5 bytes/token
//	p50k_base    ~4.2 bytes/token
//
// This avoids shipping the multi-megabyte BPE vocabulary tables. For budget
// estimation it is accurate to within a few percent of real provider counts.
package tokenizer

import (
	"bufio"
	"io"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var (
	bufPool = sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, 4096)
			return &b
		},
	}
)

// Encoding identifies a BPE variant by name.
type Encoding string

// Supported encodings.
const (
	CL100KBase Encoding = "cl100k_base"
	O200KBase  Encoding = "o200k_base"
	P50KBase   Encoding = "p50k_base"
)

// Tokenizer counts tokens for a given encoding.
type Tokenizer struct {
	Encoding Encoding
}

// New constructs a Tokenizer with the given encoding. An empty encoding
// defaults to CL100KBase.
func New(enc Encoding) *Tokenizer {
	if enc == "" {
		enc = CL100KBase
	}
	return &Tokenizer{Encoding: enc}
}

// CountString returns the approximate token count of s.
func (t *Tokenizer) CountString(s string) int {
	return CountApprox(s, t.Encoding)
}

// CountReader streams r line-by-line and returns the cumulative token count.
func (t *Tokenizer) CountReader(r io.Reader) (int, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	total := 0
	for scanner.Scan() {
		total += CountApprox(scanner.Text(), t.Encoding)
	}
	if err := scanner.Err(); err != nil {
		return total, err
	}
	return total, nil
}

// CountApprox returns the approximate token count of text under enc.
func CountApprox(text string, enc Encoding) int {
	if text == "" {
		return 0
	}
	ratio := ratioFor(enc)
	bytes := len(text)
	if ratio == 0 {
		ratio = 4.0
	}
	tokens := int(float64(bytes) / ratio)
	if tokens == 0 && bytes > 0 {
		tokens = 1
	}
	return tokens
}

// CountWords returns the number of whitespace-separated words in text.
func CountWords(text string) int {
	count := 0
	inWord := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if inWord {
				count++
				inWord = false
			}
		} else {
			inWord = true
		}
	}
	if inWord {
		count++
	}
	return count
}

// EstimateLines returns the number of lines in text. Trailing newlines do
// not contribute extra empty lines.
func EstimateLines(text string) int {
	if text == "" {
		return 0
	}
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

// TruncateToTokens returns text truncated to roughly maxTokens tokens. If
// maxTokens is <= 0 the input is returned unchanged.
func TruncateToTokens(text string, maxTokens int, enc Encoding) string {
	if maxTokens <= 0 {
		return text
	}
	ratio := ratioFor(enc)
	maxBytes := int(float64(maxTokens) * ratio)
	if len(text) <= maxBytes {
		return text
	}
	for maxBytes > 0 && !utf8.RuneStart(text[maxBytes]) {
		maxBytes--
	}
	return text[:maxBytes] + "\n// ... [truncated by tokenless]"
}

func ratioFor(enc Encoding) float64 {
	switch enc {
	case CL100KBase:
		return 4.0
	case O200KBase:
		return 3.5
	case P50KBase:
		return 4.2
	}
	return 4.0
}