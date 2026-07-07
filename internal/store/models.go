package store

import (
	"fmt"
	"strconv"
	"strings"
)

// SymbolRecord is a structural code symbol extracted from a source file.
type SymbolRecord struct {
	Name      string
	FilePath  string
	Kind      string
	Scope     string
	StartLine int
	EndLine   int
}

// FormatSymbolID returns a stable identifier derived from indexed symbol fields.
// Format: {file_path}#{kind}#{name}#{start_line}
func FormatSymbolID(rec SymbolRecord) string {
	return fmt.Sprintf("%s#%s#%s#%d", rec.FilePath, rec.Kind, rec.Name, rec.StartLine)
}

// ParseSymbolID parses a symbol ID produced by FormatSymbolID.
// Parsing is right-anchored so file paths or names may contain '#'.
func ParseSymbolID(id string) (SymbolRecord, error) {
	last := strings.LastIndex(id, "#")
	if last < 0 {
		return SymbolRecord{}, fmt.Errorf("invalid symbol id %q: missing separators", id)
	}
	startLine, err := strconv.Atoi(id[last+1:])
	if err != nil {
		return SymbolRecord{}, fmt.Errorf("invalid symbol id %q: bad start_line: %w", id, err)
	}

	rest := id[:last]
	nameSep := strings.LastIndex(rest, "#")
	if nameSep < 0 {
		return SymbolRecord{}, fmt.Errorf("invalid symbol id %q: missing separators", id)
	}
	name := rest[nameSep+1:]

	rest = rest[:nameSep]
	kindSep := strings.LastIndex(rest, "#")
	if kindSep < 0 {
		return SymbolRecord{}, fmt.Errorf("invalid symbol id %q: missing separators", id)
	}
	kind := rest[kindSep+1:]
	filePath := rest[:kindSep]

	if filePath == "" || kind == "" || name == "" {
		return SymbolRecord{}, fmt.Errorf("invalid symbol id %q: empty field", id)
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
