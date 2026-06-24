package ast

import (
	"regexp"
	"strings"
)

// CollapseMarker is exported so downstream packages (squeeze) can reuse
// the same literal when reconstructing collapsed function bodies.
const CollapseMarker = "// ... [Cuerpo colapsado por Gist para optimizar contexto] ..."

// importLikeRe matches import/require/use/package statements at column 0.
var importLikeRe = regexp.MustCompile(
	`^[ \t]*(?:import|require(?:\s*\(|from)|from\s+[A-Za-z_]|use\s+|package\s+|#include|#import|#pragma)`,
)

// commentRe matches the start of any comment line.
var commentRe = regexp.MustCompile(`^[ \t]*(?://|/\*|\*|#|--)`)

// sigKeywords lists prefixes that strongly indicate a top-level
// declaration in a common non-Go language. Order matters: longer
// multi-word prefixes come first so HasPrefix matches greedily.
var sigKeywords = []string{
	"async def ",
	"async function ",
	"export default ",
	"public static ",
	"private static ",
	"protected static ",
	"function ",
	"const ",
	"let ",
	"var ",
	"def ",
	"class ",
	"fn ",
	"func ",
	"pub ",
	"private ",
	"public ",
	"protected ",
	"static ",
	"export ",
	"struct ",
	"interface ",
	"trait ",
	"impl ",
	"enum ",
	"module ",
	"abstract ",
	"final ",
	"override ",
}

// PruneNonGo reduces a non-Go source file to its signatures plus import
// declarations. Bodies are replaced with CollapseMarker. Supported
// languages: python, javascript, typescript, rust, java, c, cpp, ruby.
// Unknown languages and structured formats (json/yaml/toml/markdown)
// pass through with a hard truncation cap.
func PruneNonGo(src, language string, maxLinesBody int) string {
	if src == "" {
		return src
	}
	if language == "" {
		language = "text"
	}
	switch language {
	case "python", "javascript", "typescript", "rust", "java", "c", "cpp", "ruby":
		return signaturePrune(src)
	case "markdown", "json", "yaml", "toml":
		return src
	default:
		return truncateLines(src, maxLinesBody)
	}
}

func signaturePrune(src string) string {
	var out strings.Builder
	lines := strings.Split(src, "\n")
	inBraceBody := 0
	inIndentBody := -1

	emit := func(s string) {
		if s != "" {
			out.WriteString(s)
			out.WriteString("\n")
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			if inBraceBody == 0 && inIndentBody < 0 {
				out.WriteString("\n")
			}
			continue
		}

		if importLikeRe.MatchString(line) {
			emit(line)
			continue
		}

		// Nested signature inside a body: close the body first, then re-process
		// the line as a fresh top-level declaration.
		if (inBraceBody > 0 || inIndentBody >= 0) && isSignature(trimmed) {
			inBraceBody = 0
			inIndentBody = -1
		}

		if inBraceBody > 0 {
			delta := strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			inBraceBody += delta
			if inBraceBody <= 0 {
				inBraceBody = 0
				continue // skip the closing brace line
			}
			continue
		}

		if inIndentBody >= 0 {
			ind := leadingWhitespace(line)
			if ind > inIndentBody {
				continue
			}
			inIndentBody = -1
		}

		if commentRe.MatchString(line) {
			emit(line)
			continue
		}

		if isSignature(trimmed) {
			emit(line)
			opens := strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			if opens > 0 {
				inBraceBody = opens
				out.WriteString(" ")
				out.WriteString(CollapseMarker)
				out.WriteString("\n")
				continue
			}
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, "://") {
				inIndentBody = leadingWhitespace(line)
				if inIndentBody < 0 {
					inIndentBody = 0
				}
				out.WriteString(" ")
				out.WriteString(CollapseMarker)
				out.WriteString("\n")
				continue
			}
			continue
		}

		emit(line)
	}
	return out.String()
}

// isSignature reports whether line (already trimmed) looks like a
// declaration that opens a body. The keyword prefix list covers most
// languages; the fallback heuristic catches C-style bare-type signatures
// like `int main(int argc, char** argv) {`.
func isSignature(line string) bool {
	if line == "" || commentRe.MatchString(line) {
		return false
	}
	for _, kw := range sigKeywords {
		if strings.HasPrefix(line, kw) {
			return true
		}
	}
	// Fallback: at column 0, contains a `(...)` group and ends with `{` / `:`.
	if leadingWhitespace(line) == 0 &&
		strings.Contains(line, "(") &&
		strings.Contains(line, ")") {
		tail := trimmedTail(line)
		if tail == "{" || tail == ":" {
			return true
		}
		// Patterns like `int main(int argc) {` or `Foo(int x):`.
		if strings.Contains(line, ") {") || strings.Contains(line, "):") {
			return true
		}
	}
	return false
}

func trimmedTail(line string) string {
	end := len(line)
	for end > 0 {
		r := line[end-1]
		if r == ' ' || r == '\t' {
			end--
			continue
		}
		break
	}
	return line[:end]
}

func leadingWhitespace(s string) int {
	for i, r := range s {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return len(s)
}

func truncateLines(src string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 50
	}
	lines := strings.Split(src, "\n")
	if len(lines) <= maxLines {
		return src
	}
	return strings.Join(lines[:maxLines], "\n") + "\n// ... [truncated by gist]"
}