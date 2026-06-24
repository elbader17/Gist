// Package ast implements Go AST pruning and skeleton generation for the
// view_file_slim tool.
//
// The Pruner uses the standard library go/parser + go/printer to transform
// a Go source file by collapsing function bodies into a single marker
// expression. Function signatures, type declarations, and imports are
// preserved exactly.
//
// For non-Go files, the BuildSlim helper truncates line content to a
// configurable budget.
package ast

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// collapseMarker is the literal inserted in place of each function body.
//
// It is exported as a const for testing but should not be relied upon by
// external callers.
const collapseMarker = "// ... [Cuerpo colapsado por Gist para optimizar contexto] ..."

// PruneOptions controls the Pruner behaviour.
type PruneOptions struct {
	FilePath       string
	FocusFunctions []string
	MaxLinesBody   int
}

// Pruner holds the file set used for printing back to source after pruning.
type Pruner struct {
	fset *token.FileSet
}

// NewPruner constructs a Pruner with a fresh token.FileSet.
func NewPruner() *Pruner {
	return &Pruner{fset: token.NewFileSet()}
}

// IsGoFile reports whether the path has a .go extension.
func (p *Pruner) IsGoFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".go")
}

// PruneGoFile parses src as Go source and returns the source with all
// non-focused function bodies collapsed. focusFunctions lists names that
// should remain expanded.
func (p *Pruner) PruneGoFile(src []byte, opts PruneOptions) (string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, opts.FilePath, src, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	focus := make(map[string]bool, len(opts.FocusFunctions))
	for _, fn := range opts.FocusFunctions {
		focus[fn] = true
	}

	for _, decl := range file.Decls {
		p.pruneDecl(decl, focus)
	}

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, file); err != nil {
		return "", fmt.Errorf("print: %w", err)
	}

	out := buf.String()
	if opts.MaxLinesBody > 0 {
		out = p.truncateBodies(out, opts.MaxLinesBody)
	}
	return out, nil
}

// Skeleton returns a one-line-per-decl summary of a Go file. Useful when
// even the pruned form is too large.
func (p *Pruner) Skeleton(src []byte, filePath string) (string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("package ")
	b.WriteString(file.Name.Name)
	b.WriteString("\n\n")

	if file.Imports != nil {
		b.WriteString("import (\n")
		for _, imp := range file.Imports {
			b.WriteString("\t")
			if imp.Name != nil {
				b.WriteString(imp.Name.Name)
				b.WriteString(" ")
			}
			b.WriteString(imp.Path.Value)
			b.WriteString("\n")
		}
		b.WriteString(")\n\n")
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			b.WriteString(renderFuncDecl(fset, d))
			b.WriteString("\n")
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						b.WriteString("type ")
						b.WriteString(ts.Name.Name)
						b.WriteString(" ")
						switch ts.Type.(type) {
						case *ast.StructType:
							b.WriteString("struct { ... }\n")
						case *ast.InterfaceType:
							b.WriteString("interface { ... }\n")
						default:
							b.WriteString("... }\n")
						}
					}
				}
			}
		}
	}
	return b.String(), nil
}

func (p *Pruner) pruneDecl(decl ast.Decl, focus map[string]bool) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if focus[d.Name.Name] {
			return
		}
		if d.Body == nil {
			return
		}
		p.collapseBody(d.Body)
	case *ast.GenDecl:
		if d.Tok != token.TYPE {
			return
		}
		for _, spec := range d.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			p.collapseTypeBody(ts)
		}
	}
}

func (p *Pruner) collapseTypeBody(ts *ast.TypeSpec) {
	switch t := ts.Type.(type) {
	case *ast.StructType:
		if t.Fields == nil {
			return
		}
		t.Fields.List = p.filterFields(t.Fields.List)
	case *ast.InterfaceType:
		if t.Methods == nil {
			return
		}
		t.Methods.List = nil
	}
}

func (p *Pruner) filterFields(fields []*ast.Field) []*ast.Field {
	kept := fields[:0]
	for _, f := range fields {
		if f.Tag != nil {
			tag := strings.Trim(f.Tag.Value, "`")
			if strings.Contains(strings.ToLower(tag), "json:\"-\"") {
				continue
			}
		}
		kept = append(kept, f)
	}
	return kept
}

func (p *Pruner) collapseBody(body *ast.BlockStmt) {
	body.List = []ast.Stmt{
		&ast.ExprStmt{
			X: &ast.BasicLit{
				Kind:  token.STRING,
				Value: "`" + collapseMarker + "`",
			},
		},
	}
}

func (p *Pruner) truncateBodies(src string, maxLines int) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	inBody := false
	bodyLines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "{") {
			inBody = true
			bodyLines = 0
			out = append(out, line)
			continue
		}
		if inBody {
			bodyLines++
			if trimmed == "}" {
				inBody = false
				out = append(out, line)
				continue
			}
			if bodyLines > maxLines {
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func renderFuncDecl(fset *token.FileSet, d *ast.FuncDecl) string {
	var buf bytes.Buffer
	if d.Recv != nil {
		buf.WriteString("func (")
		if len(d.Recv.List) > 0 {
			r := d.Recv.List[0]
			if len(r.Names) > 0 {
				buf.WriteString(r.Names[0].Name)
				buf.WriteString(" ")
			}
			printer.Fprint(&buf, fset, r.Type)
		}
		buf.WriteString(") ")
	} else {
		buf.WriteString("func ")
	}
	buf.WriteString(d.Name.Name)
	buf.WriteString("(")
	if d.Type.Params != nil {
		printer.Fprint(&buf, fset, d.Type.Params)
	}
	buf.WriteString(")")
	if d.Type.Results != nil {
		buf.WriteString(" ")
		printer.Fprint(&buf, fset, d.Type.Results)
	}
	if d.Body == nil {
		buf.WriteString("\n")
		return buf.String()
	}
	buf.WriteString(" { /* ... */ }\n")
	return buf.String()
}