//go:build treesitter

package treesitter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	sitter "github.com/smacker/go-tree-sitter"
	golang "github.com/smacker/go-tree-sitter/golang"
	python "github.com/smacker/go-tree-sitter/python"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/stubbies/litos-mcp/internal/store"
)

// CallSitesEnabled reports whether tree-sitter call extraction is available.
func CallSitesEnabled() bool { return true }

// CallSiteExtensions returns extensions with tree-sitter call extraction support.
func CallSiteExtensions() []string {
	exts := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".pyw"}
	sort.Strings(exts)
	return exts
}

// ExtractCallSites walks call_expression / call AST nodes for supported languages.
func ExtractCallSites(repoRoot string, paths []string) ([]store.CallSiteRecord, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("treesitter calls: abs root: %w", err)
	}

	var all []store.CallSiteRecord
	for _, rel := range paths {
		rel = filepath.ToSlash(rel)
		lang := languageForExt(rel)
		if lang == langUnknown {
			continue
		}

		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("treesitter calls: read %s: %w", rel, err)
		}

		calls, err := parseCallSites(source, rel, lang)
		if err != nil {
			return nil, fmt.Errorf("treesitter calls: parse %s: %w", rel, err)
		}
		all = append(all, calls...)
	}
	return all, nil
}

func parseCallSites(source []byte, relPath string, lang langKind) ([]store.CallSiteRecord, error) {
	parser := sitter.NewParser()
	defer parser.Close()

	switch lang {
	case langGo:
		parser.SetLanguage(golang.GetLanguage())
	case langTS:
		parser.SetLanguage(typescript.GetLanguage())
	case langPy:
		parser.SetLanguage(python.GetLanguage())
	default:
		return nil, nil
	}

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	var calls []store.CallSiteRecord
	collectCallSites(tree.RootNode(), source, lang, relPath, &calls)
	return calls, nil
}

func collectCallSites(node *sitter.Node, source []byte, lang langKind, relPath string, out *[]store.CallSiteRecord) {
	if node == nil {
		return
	}

	switch {
	case lang == langPy && node.Type() == "call":
		if fn := node.ChildByFieldName("function"); fn != nil {
			appendCallSite(out, fn, source, relPath, lang)
		}
	case (lang == langGo || lang == langTS) && node.Type() == "call_expression":
		if fn := node.ChildByFieldName("function"); fn != nil {
			appendCallSite(out, fn, source, relPath, lang)
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		collectCallSites(node.Child(i), source, lang, relPath, out)
	}
}

func appendCallSite(out *[]store.CallSiteRecord, fn *sitter.Node, source []byte, relPath string, lang langKind) {
	name, startByte, ok := calleeNameAt(fn, source, lang)
	if !ok || name == "" {
		return
	}
	line, _ := linesForRange(source, startByte, startByte+1)
	*out = append(*out, store.CallSiteRecord{
		CalleeName: name,
		FilePath:   relPath,
		Line:       line,
		Col:        columnForByte(source, startByte),
	})
}

func calleeNameAt(node *sitter.Node, source []byte, lang langKind) (name string, startByte int, ok bool) {
	if node == nil {
		return "", 0, false
	}

	switch node.Type() {
	case "identifier":
		return node.Content(source), int(node.StartByte()), true
	case "selector_expression":
		if lang == langGo {
			if field := node.ChildByFieldName("field"); field != nil {
				return field.Content(source), int(field.StartByte()), true
			}
		}
	case "member_expression":
		if lang == langTS {
			if prop := node.ChildByFieldName("property"); prop != nil && prop.Type() == "property_identifier" {
				return prop.Content(source), int(prop.StartByte()), true
			}
		}
	case "attribute":
		if lang == langPy {
			if attr := node.ChildByFieldName("attribute"); attr != nil {
				return attr.Content(source), int(attr.StartByte()), true
			}
		}
	case "parenthesized_expression":
		for i := 0; i < int(node.NamedChildCount()); i++ {
			if name, startByte, ok = calleeNameAt(node.NamedChild(i), source, lang); ok {
				return name, startByte, true
			}
		}
	case "subscript_expression":
		// obj[key]() — use the object expression's callee name when present.
		if obj := node.ChildByFieldName("object"); obj != nil {
			return calleeNameAt(obj, source, lang)
		}
	}

	return "", 0, false
}

func columnForByte(source []byte, byteOffset int) int {
	if byteOffset < 0 {
		byteOffset = 0
	}
	if byteOffset > len(source) {
		byteOffset = len(source)
	}
	lineStart := byteOffset
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	return byteOffset - lineStart + 1
}
