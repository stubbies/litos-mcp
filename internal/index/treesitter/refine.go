//go:build treesitter

package treesitter

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	golang "github.com/smacker/go-tree-sitter/golang"
	python "github.com/smacker/go-tree-sitter/python"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/stubbies/litos-mcp/internal/store"
)

// Enabled reports whether tree-sitter boundary refinement is compiled in.
func Enabled() bool { return true }

type langKind int

const (
	langUnknown langKind = iota
	langGo
	langTS
	langPy
)

type defCandidate struct {
	name      string
	matchLine int
	startLine int
	endLine   int
	startByte int
	endByte   int
}

// RefineBoundaries adjusts symbol line and byte ranges using tree-sitter AST nodes.
func RefineBoundaries(repoRoot string, symbols []store.SymbolRecord) ([]store.SymbolRecord, error) {
	if len(symbols) == 0 {
		return symbols, nil
	}

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("treesitter: abs root: %w", err)
	}

	byFile := make(map[string][]int)
	for i, sym := range symbols {
		byFile[sym.FilePath] = append(byFile[sym.FilePath], i)
	}

	out := make([]store.SymbolRecord, len(symbols))
	copy(out, symbols)

	for relPath, indices := range byFile {
		lang := languageForExt(relPath)
		if lang == langUnknown {
			continue
		}

		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("treesitter: read %s: %w", relPath, err)
		}

		candidates, err := parseDefinitions(source, lang)
		if err != nil {
			return nil, fmt.Errorf("treesitter: parse %s: %w", relPath, err)
		}
		if len(candidates) == 0 {
			continue
		}

		for _, idx := range indices {
			if match, ok := bestMatch(out[idx], candidates); ok {
				out[idx].StartLine = match.startLine
				out[idx].EndLine = match.endLine
				out[idx].StartByte = match.startByte
				out[idx].EndByte = match.endByte
			}
		}
	}

	return out, nil
}

func languageForExt(relPath string) langKind {
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".go":
		return langGo
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return langTS
	case ".py", ".pyw":
		return langPy
	default:
		return langUnknown
	}
}

func parseDefinitions(source []byte, lang langKind) ([]defCandidate, error) {
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

	var candidates []defCandidate
	collectDefinitions(tree.RootNode(), nil, source, lang, &candidates)
	return candidates, nil
}

func collectDefinitions(node, parent *sitter.Node, source []byte, lang langKind, out *[]defCandidate) {
	if node == nil {
		return
	}

	nodeType := node.Type()
	if nodeType == "decorated_definition" && lang == langPy {
		if cand, ok := decoratedDefCandidate(node, source); ok {
			*out = append(*out, cand)
		}
	} else if nodeType == "type_declaration" && lang == langGo {
		collectGoTypeSpecs(node, source, out)
	} else if isDefinitionNode(nodeType, lang) {
		if parent != nil && parent.Type() == "decorated_definition" {
			// Inner definition is represented by the decorated_definition candidate.
		} else if name := extractName(node, source); name != "" {
			appendCandidate(out, node, source, name)
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		collectDefinitions(node.Child(i), node, source, lang, out)
	}
}

func decoratedDefCandidate(node *sitter.Node, source []byte) (defCandidate, bool) {
	def := node.ChildByFieldName("definition")
	if def == nil {
		return defCandidate{}, false
	}
	name := extractName(def, source)
	if name == "" {
		return defCandidate{}, false
	}
	startByte := int(node.StartByte())
	endByte := int(node.EndByte())
	matchLine, _ := linesForRange(source, int(def.StartByte()), int(def.StartByte())+1)
	startLine, endLine := linesForRange(source, startByte, endByte)
	return defCandidate{
		name:      name,
		matchLine: matchLine,
		startLine: startLine,
		endLine:   endLine,
		startByte: startByte,
		endByte:   endByte,
	}, true
}

func collectGoTypeSpecs(node *sitter.Node, source []byte, out *[]defCandidate) {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type() != "type_spec" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		appendCandidate(out, child, source, nameNode.Content(source))
	}
}

func appendCandidate(out *[]defCandidate, node *sitter.Node, source []byte, name string) {
	startByte := int(node.StartByte())
	endByte := int(node.EndByte())
	startLine, endLine := linesForRange(source, startByte, endByte)
	*out = append(*out, defCandidate{
		name:      name,
		matchLine: startLine,
		startLine: startLine,
		endLine:   endLine,
		startByte: startByte,
		endByte:   endByte,
	})
}

func isDefinitionNode(nodeType string, lang langKind) bool {
	switch lang {
	case langGo:
		switch nodeType {
		case "function_declaration", "method_declaration":
			return true
		}
	case langTS:
		switch nodeType {
		case "function_declaration", "class_declaration", "interface_declaration", "variable_declarator":
			return true
		}
	case langPy:
		switch nodeType {
		case "function_definition", "class_definition":
			return true
		}
	}
	return false
}

func extractName(node *sitter.Node, source []byte) string {
	switch node.Type() {
	case "function_declaration", "method_declaration", "class_declaration", "interface_declaration",
		"function_definition", "class_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			return nameNode.Content(source)
		}
	case "variable_declarator":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			return nameNode.Content(source)
		}
	}
	return ""
}

func linesForRange(source []byte, startByte, endByte int) (int, int) {
	if startByte < 0 {
		startByte = 0
	}
	if endByte > len(source) {
		endByte = len(source)
	}
	if startByte > len(source) {
		startByte = len(source)
	}
	startLine := 1 + bytes.Count(source[:startByte], []byte{'\n'})
	endLine := startLine + bytes.Count(source[startByte:endByte], []byte{'\n'})
	return startLine, endLine
}

func bestMatch(sym store.SymbolRecord, candidates []defCandidate) (defCandidate, bool) {
	var best *defCandidate
	bestDist := 2

	for i := range candidates {
		c := &candidates[i]
		if c.name != sym.Name {
			continue
		}
		dist := abs(c.matchLine - sym.StartLine)
		if dist > 1 {
			continue
		}
		if best == nil || dist < bestDist || (dist == bestDist && c.matchLine == sym.StartLine) {
			best = c
			bestDist = dist
		}
	}

	if best == nil {
		return defCandidate{}, false
	}
	return *best, true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
