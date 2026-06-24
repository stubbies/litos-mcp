package store

// SymbolRecord is a structural code symbol extracted from a source file.
type SymbolRecord struct {
	Name      string
	FilePath  string
	Kind      string
	Scope     string
	StartLine int
	EndLine   int
}

// SearchHit is a single FTS search result with line-range metadata.
type SearchHit struct {
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
	// MatchMode is "and" (default) or "or" for multi-token queries.
	MatchMode string
}

// FileMeta tracks indexed file identity for incremental reindexing.
type FileMeta struct {
	Path    string
	MtimeNs int64
	Size    int64
}
