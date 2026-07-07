package store

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrInvalidSymbolID indicates a symbol_id string could not be parsed.
	ErrInvalidSymbolID = errors.New("invalid symbol id")
	// ErrSymbolNotFound indicates no indexed symbol matches the given symbol_id.
	ErrSymbolNotFound = errors.New("symbol not found")
)

// SymbolRecord is a structural code symbol extracted from a source file.
type SymbolRecord struct {
	Name      string
	FilePath  string
	Kind      string
	Scope     string
	StartLine int
	EndLine   int
	StartByte int // -1 when unset (stored as NULL)
	EndByte   int // -1 when unset (stored as NULL)
}

// FormatSymbolID returns a stable identifier derived from indexed symbol fields.
// Format: {file_path}#{kind}#{name}#{start_line}
// Symbol names must not contain '#'; file paths may.
func FormatSymbolID(rec SymbolRecord) string {
	return fmt.Sprintf("%s#%s#%s#%d", rec.FilePath, rec.Kind, rec.Name, rec.StartLine)
}

// ParseSymbolID parses a symbol ID produced by FormatSymbolID.
// Parsing is right-anchored so file paths may contain '#'; symbol names must not.
func ParseSymbolID(id string) (SymbolRecord, error) {
	last := strings.LastIndex(id, "#")
	if last < 0 {
		return SymbolRecord{}, fmt.Errorf("%w: %q: missing separators", ErrInvalidSymbolID, id)
	}
	startLine, err := strconv.Atoi(id[last+1:])
	if err != nil {
		return SymbolRecord{}, fmt.Errorf("%w: %q: bad start_line: %v", ErrInvalidSymbolID, id, err)
	}

	rest := id[:last]
	nameSep := strings.LastIndex(rest, "#")
	if nameSep < 0 {
		return SymbolRecord{}, fmt.Errorf("%w: %q: missing separators", ErrInvalidSymbolID, id)
	}
	name := rest[nameSep+1:]

	rest = rest[:nameSep]
	kindSep := strings.LastIndex(rest, "#")
	if kindSep < 0 {
		return SymbolRecord{}, fmt.Errorf("%w: %q: missing separators", ErrInvalidSymbolID, id)
	}
	kind := rest[kindSep+1:]
	filePath := rest[:kindSep]

	if filePath == "" || kind == "" || name == "" {
		return SymbolRecord{}, fmt.Errorf("%w: %q: empty field", ErrInvalidSymbolID, id)
	}

	return SymbolRecord{
		FilePath:  filePath,
		Kind:      kind,
		Name:      name,
		StartLine: startLine,
	}, nil
}

// OutlineEntry is a symbol in a single-file outline (search hit shape without matched_in).
type OutlineEntry struct {
	SymbolID  string `json:"symbol_id"`
	FilePath  string `json:"file_path"`
	Symbol    string `json:"symbol"`
	Kind      string `json:"kind"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Scope     string `json:"scope"`
}

// OutlineEntryFromRecord builds an outline entry from an indexed symbol.
func OutlineEntryFromRecord(rec SymbolRecord) OutlineEntry {
	return OutlineEntry{
		SymbolID:  FormatSymbolID(rec),
		FilePath:  rec.FilePath,
		Symbol:    rec.Name,
		Kind:      rec.Kind,
		StartLine: rec.StartLine,
		EndLine:   rec.EndLine,
		Scope:     rec.Scope,
	}
}

// SearchHit is a single FTS search result with line-range metadata.
type SearchHit struct {
	SymbolID  string `json:"symbol_id"`
	FilePath  string `json:"file_path"`
	Symbol    string `json:"symbol"`
	Kind      string `json:"kind"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Scope     string `json:"scope"`
	MatchedIn string `json:"matched_in"`
}

// SearchOptions configures search behavior.
type SearchOptions struct {
	// MatchMode is "and" (default) or "or" for multi-token FTS queries.
	MatchMode string
	// NameMatch selects symbol name lookup instead of FTS when set to "exact" or "contains".
	// When set, Query is treated as a symbol name (case-sensitive for exact).
	NameMatch string
}

// FileMeta tracks indexed file identity for incremental reindexing.
type FileMeta struct {
	Path    string
	MtimeNs int64
	Size    int64
}

// CallSiteRecord is a callee invocation extracted from a source file.
type CallSiteRecord struct {
	CalleeName     string
	FilePath       string
	Line           int
	Col            int
	EnclosingName  string
	EnclosingKind  string
	EnclosingScope string
}

// DirectoryMap is a directory-level architecture sketch: definitions and outgoing calls.
type DirectoryMap struct {
	Dir               string             `json:"dir"`
	Definitions       []OutlineEntry     `json:"definitions"`
	OutgoingCalls     []OutgoingCallEntry `json:"outgoing_calls"`
	DefinitionCount   int                `json:"definition_count"`
	OutgoingCallCount int                `json:"outgoing_call_count"`
}

// OutgoingCallEntry is a callee invocation originating from files under a directory prefix.
type OutgoingCallEntry struct {
	CalleeName        string `json:"callee_name"`
	FilePath          string `json:"file_path"`
	Line              int    `json:"line"`
	EnclosingSymbolID string `json:"enclosing_symbol_id,omitempty"`
}

// CallerHit is a JSON-serializable find_callers result.
type CallerHit struct {
	CalleeName        string `json:"callee_name"`
	FilePath          string `json:"file_path"`
	Line              int    `json:"line"`
	Col               int    `json:"col"`
	EnclosingSymbol   string `json:"enclosing_symbol"`
	EnclosingKind     string `json:"enclosing_kind"`
	EnclosingScope    string `json:"enclosing_scope"`
	EnclosingSymbolID string `json:"enclosing_symbol_id,omitempty"`
}

// ResolveEnclosingSymbol returns the innermost indexed symbol enclosing line.
// On overlap, the symbol with the smallest line span wins.
func ResolveEnclosingSymbol(symbols []SymbolRecord, line int) (name, kind, scope string) {
	var best *SymbolRecord
	bestSpan := -1
	for i := range symbols {
		sym := &symbols[i]
		if sym.StartLine <= line && line <= sym.EndLine {
			span := sym.EndLine - sym.StartLine
			if best == nil || span < bestSpan {
				best = sym
				bestSpan = span
			}
		}
	}
	if best == nil {
		return "", "", ""
	}
	return best.Name, best.Kind, best.Scope
}
