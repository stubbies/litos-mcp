//go:build treesitter

package treesitter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	golang "github.com/smacker/go-tree-sitter/golang"
	python "github.com/smacker/go-tree-sitter/python"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// ParseError describes a syntax error location in a source file.
type ParseError struct {
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

// CheckSyntaxEnabled reports whether tree-sitter syntax checking is available.
func CheckSyntaxEnabled() bool { return true }

// CheckSyntax parses a file and returns ERROR nodes from the tree-sitter AST.
func CheckSyntax(repoRoot, relPath string) ([]ParseError, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("treesitter check: abs root: %w", err)
	}

	relPath = filepath.ToSlash(relPath)
	lang := languageForExt(relPath)
	if lang == langUnknown {
		return nil, fmt.Errorf("unsupported file extension for syntax check")
	}

	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found")
		}
		return nil, fmt.Errorf("treesitter check: read %s: %w", relPath, err)
	}

	return parseSyntaxErrors(source, lang)
}

func parseSyntaxErrors(source []byte, lang langKind) ([]ParseError, error) {
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

	var errors []ParseError
	collectSyntaxErrors(tree.RootNode(), source, &errors)
	return errors, nil
}

func collectSyntaxErrors(node *sitter.Node, source []byte, out *[]ParseError) {
	if node == nil {
		return
	}

	if node.Type() == "ERROR" || node.IsMissing() {
		appendParseError(out, node, source)
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		collectSyntaxErrors(node.Child(i), source, out)
	}
}

func appendParseError(out *[]ParseError, node *sitter.Node, source []byte) {
	startByte := int(node.StartByte())
	line, _ := linesForRange(source, startByte, startByte+1)
	*out = append(*out, ParseError{
		Line:    line,
		Col:     columnForByte(source, startByte),
		Message: syntaxErrorMessage(node, source),
	})
}

func syntaxErrorMessage(node *sitter.Node, source []byte) string {
	if node.IsMissing() {
		return "missing " + node.Type()
	}
	text := strings.TrimSpace(node.Content(source))
	if text == "" {
		return "syntax error"
	}
	const maxLen = 80
	if len(text) > maxLen {
		return text[:maxLen] + "..."
	}
	return text
}
