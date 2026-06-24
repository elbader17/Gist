package aligner

import (
	"fmt"
	"strings"
)

// RenderMarkdown concatenates an aligned payload into a single
// cache-friendly markdown document with explicit per-layer markers.
//
// Format:
//
//	<!-- layer:1:system_rules:<8-hex-hash> -->
//	<content>
//	<!-- layer:2:static_files:<8-hex-hash> -->
//	<content>
//	...
//
// Identical content produces byte-identical output, so provider-side
// prompt caches can reuse the same prefix across calls.
func RenderMarkdown(p *AlignedPayload) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	for _, block := range p.Blocks {
		if block.Content == "" {
			continue
		}
		fmt.Fprintf(&b, "<!-- layer:%d:%s:%s -->\n", block.Layer, block.LayerName, block.StableHash)
		b.WriteString(block.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// DedupRatio computes the fraction of input bytes removed by deduping
// and sorting the supplied components. Returns 0 when the input is empty
// or the joined output is larger than the input (should not happen).
func DedupRatio(input []string, joined string) float64 {
	total := 0
	for _, s := range input {
		total += len(s)
	}
	if total == 0 {
		return 0
	}
	out := len(joined)
	if out >= total {
		return 0
	}
	return 1 - float64(out)/float64(total)
}

// BlockHash returns the 8-byte sha256 hash used for cache-key stability.
// Exported for downstream packages (squeeze, metrics) that need to label
// individual layers.
func BlockHash(s string) string {
	return hashBlock(s)
}