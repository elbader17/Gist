package ast

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkeletonResult is the JSON payload returned by BuildSlim.
type SkeletonResult struct {
	FilePath   string `json:"file_path"`
	Language   string `json:"language"`
	Original   string `json:"original_content,omitempty"`
	Slim       string `json:"slim_content"`
	Truncated  bool   `json:"truncated"`
	FocusedOut []string `json:"focused_out,omitempty"`
}

// BuildSlim reads filePath and returns its pruned form. For Go files it uses
// the Pruner; for any other extension it truncates to maxLinesBody lines
// (default 50).
func BuildSlim(filePath string, focusFunctions []string, maxLinesBody int) (*SkeletonResult, error) {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(f)
	pruner := NewPruner()

	res := &SkeletonResult{
		FilePath: abs,
		Language: detectLanguage(abs),
	}

	if pruner.IsGoFile(abs) {
		var buf strings.Builder
		_, _ = reader.WriteTo(stringBuilderWriter{&buf})
		src := buf.String()

		slim, err := pruner.PruneGoFile([]byte(src), PruneOptions{
			FilePath:       abs,
			FocusFunctions: focusFunctions,
			MaxLinesBody:   maxLinesBody,
		})
		if err != nil {
			return nil, err
		}
		res.Slim = slim
	} else {
		var lines []string
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		limit := maxLinesBody
		if limit <= 0 {
			limit = 50
		}
		if len(lines) > limit {
			res.Truncated = true
			lines = lines[:limit]
			lines = append(lines, "// ... [truncated by gist]")
		}
		res.Slim = strings.Join(lines, "\n")
	}

	if info.Size() > 0 {
		res.Original = fmt.Sprintf("%d bytes", info.Size())
	}
	return res, nil
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	default:
		return "text"
	}
}

type stringBuilderWriter struct {
	b *strings.Builder
}

func (w stringBuilderWriter) Write(p []byte) (int, error) {
	return w.b.Write(p)
}