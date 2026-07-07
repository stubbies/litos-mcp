package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

const CacheDBName = ".lcn_cache.db"

// ErrFTS5Unavailable indicates the SQLite build lacks FTS5 support.
var ErrFTS5Unavailable = fmt.Errorf("FTS5 is not available in this SQLite build; litos-mcp requires FTS5")

// Store wraps the SQLite index database.
type Store struct {
	db   *sql.DB
	path string
	mu   sync.Mutex
}

// Open opens or creates the cache database at repoRoot/.lcn_cache.db.
// WAL mode is enabled, FTS5 availability is verified, and schema is migrated.
func Open(repoRoot string) (*Store, error) {
	dbPath := filepath.Join(repoRoot, CacheDBName)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	if err := probeFTS5(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	st := &Store{db: db, path: dbPath}
	if err := st.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return st, nil
}

// OpenMemory opens an in-memory database with the same schema (for tests).
func OpenMemory() (*Store, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open in-memory sqlite: %w", err)
	}

	if err := probeFTS5(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	st := &Store{db: db, path: ":memory:"}
	if err := st.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return st, nil
}

func (s *Store) initSchema() error {
	if _, err := s.db.Exec(schemaDDL); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if _, err := s.db.Exec(triggerDDL); err != nil {
		return fmt.Errorf("create fts triggers: %w", err)
	}
	return nil
}

// Path returns the absolute path to the database file.
func (s *Store) Path() string {
	return s.path
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the underlying connection for tests and future schema work.
func (s *Store) DB() *sql.DB {
	return s.db
}

// ProbeFTS5 verifies FTS5 support in the given database connection.
func ProbeFTS5(db *sql.DB) error {
	return probeFTS5(db)
}

func probeFTS5(db *sql.DB) error {
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS _fts_probe USING fts5(x)`); err != nil {
		return fmt.Errorf("%w: %v", ErrFTS5Unavailable, err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS _fts_probe`); err != nil {
		return fmt.Errorf("fts5 probe cleanup: %w", err)
	}
	return nil
}

// Exists reports whether the cache database file exists at repoRoot.
func Exists(repoRoot string) bool {
	path := filepath.Join(repoRoot, CacheDBName)
	_, err := os.Stat(path)
	return err == nil
}

// UpsertFile atomically replaces all symbols for a file and updates file metadata.
// One transaction per file: DELETE existing symbols → INSERT new symbols → upsert files row.
func (s *Store) UpsertFile(meta FileMeta, symbols []SymbolRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin upsert transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM symbols WHERE file_path = ?`, meta.Path); err != nil {
		return fmt.Errorf("delete symbols for %s: %w", meta.Path, err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO symbols (name, file_path, kind, scope, start_line, end_line, start_byte, end_byte)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare symbol insert: %w", err)
	}
	defer stmt.Close()

	for _, sym := range symbols {
		scope := sym.Scope
		if scope == "" {
			scope = ""
		}
		startByte, endByte := symbolByteColumns(sym)
		if _, err := stmt.Exec(sym.Name, meta.Path, sym.Kind, scope, sym.StartLine, sym.EndLine, startByte, endByte); err != nil {
			return fmt.Errorf("insert symbol %q in %s: %w", sym.Name, meta.Path, err)
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO files (path, mtime_ns, size) VALUES (?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET mtime_ns = excluded.mtime_ns, size = excluded.size
	`, meta.Path, meta.MtimeNs, meta.Size); err != nil {
		return fmt.Errorf("upsert file row for %s: %w", meta.Path, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert for %s: %w", meta.Path, err)
	}
	return nil
}

// RemoveFile deletes all symbols and the files row for a path no longer on disk.
func (s *Store) RemoveFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin remove transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM symbols WHERE file_path = ?`, path); err != nil {
		return fmt.Errorf("delete symbols for %s: %w", path, err)
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE path = ?`, path); err != nil {
		return fmt.Errorf("delete file row for %s: %w", path, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remove for %s: %w", path, err)
	}
	return nil
}

// GetFileMeta returns indexed metadata for path, or false if not indexed.
func (s *Store) GetFileMeta(path string) (FileMeta, bool, error) {
	var meta FileMeta
	meta.Path = path
	err := s.db.QueryRow(`SELECT mtime_ns, size FROM files WHERE path = ?`, path).Scan(&meta.MtimeNs, &meta.Size)
	if err == sql.ErrNoRows {
		return FileMeta{}, false, nil
	}
	if err != nil {
		return FileMeta{}, false, fmt.Errorf("get file meta for %s: %w", path, err)
	}
	return meta, true, nil
}

// IsStale reports whether path needs reindexing compared to current filesystem metadata.
func (s *Store) IsStale(path string, mtimeNs, size int64) (bool, error) {
	meta, ok, err := s.GetFileMeta(path)
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil
	}
	return meta.MtimeNs != mtimeNs || meta.Size != size, nil
}

// ListFiles returns all indexed file metadata.
func (s *Store) ListFiles() ([]FileMeta, error) {
	rows, err := s.db.Query(`SELECT path, mtime_ns, size FROM files ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()

	var files []FileMeta
	for rows.Next() {
		var meta FileMeta
		if err := rows.Scan(&meta.Path, &meta.MtimeNs, &meta.Size); err != nil {
			return nil, fmt.Errorf("scan file row: %w", err)
		}
		files = append(files, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate files: %w", err)
	}
	return files, nil
}

// SetMeta stores a key/value pair in the meta table.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	if err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}
	return nil
}

// GetMeta returns the value for key, or false if absent.
func (s *Store) GetMeta(key string) (string, bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get meta %q: %w", key, err)
	}
	return value, true, nil
}

// FileCount returns the number of indexed files.
func (s *Store) FileCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count files: %w", err)
	}
	return n, nil
}

// SymbolCount returns the number of indexed symbols.
func (s *Store) SymbolCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM symbols`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count symbols: %w", err)
	}
	return n, nil
}

// Search queries FTS5 and returns ranked hits joined with line-range metadata.
func (s *Store) Search(query string, limit int) ([]SearchHit, error) {
	return s.SearchWithOptions(query, limit, SearchOptions{})
}

// SearchWithOptions queries FTS5 with optional match mode and LIKE fallback,
// or performs exact/contains symbol name lookup when NameMatch is set.
func (s *Store) SearchWithOptions(query string, limit int, opts SearchOptions) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 10
	}

	nameMatch, err := parseNameMatch(opts.NameMatch)
	if err != nil {
		return nil, err
	}
	if nameMatch != "" {
		hits, err := s.searchByName(query, limit, nameMatch)
		if err != nil {
			return nil, err
		}
		annotateMatchedIn(query, hits)
		return hits, nil
	}

	matchMode := normalizeMatchMode(opts.MatchMode)
	ftsQuery := SanitizeFTSQuery(query, matchMode)
	hits, err := s.searchFTS(ftsQuery, limit)
	if err != nil || len(hits) == 0 {
		fallback, fbErr := s.searchLikeFallback(query, limit, matchMode)
		if fbErr != nil {
			return nil, fbErr
		}
		if err != nil && len(fallback) == 0 {
			return nil, err
		}
		hits = fallback
	}
	annotateMatchedIn(query, hits)
	return hits, nil
}

func normalizeMatchMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "or") {
		return "or"
	}
	return "and"
}

func parseNameMatch(mode string) (string, error) {
	trimmed := strings.TrimSpace(mode)
	if trimmed == "" {
		return "", nil
	}
	switch strings.ToLower(trimmed) {
	case "exact", "contains":
		return strings.ToLower(trimmed), nil
	default:
		return "", fmt.Errorf("invalid name_match %q: want exact or contains", mode)
	}
}

func (s *Store) searchByName(query string, limit int, mode string) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	var sql string
	var args []any
	if mode == "exact" {
		sql = `
			SELECT file_path, name, kind, start_line, end_line, scope
			FROM symbols
			WHERE name = ?
			ORDER BY file_path, start_line
			LIMIT ?
		`
		args = []any{query, limit}
	} else {
		pattern := "%" + escapeLike(query) + "%"
		sql = `
			SELECT file_path, name, kind, start_line, end_line, scope
			FROM symbols
			WHERE name LIKE ? ESCAPE '\'
			ORDER BY file_path, start_line
			LIMIT ?
		`
		args = []any{pattern, limit}
	}

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("name search: %w", err)
	}
	defer rows.Close()
	return scanSearchHits(rows)
}

func (s *Store) searchFTS(ftsQuery string, limit int) ([]SearchHit, error) {
	rows, err := s.db.Query(`
		SELECT s.file_path, s.name, s.kind, s.start_line, s.end_line, s.scope
		FROM symbols_fts f
		JOIN symbols s ON f.rowid = s.id
		WHERE symbols_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchHits(rows)
}

func (s *Store) searchLikeFallback(query string, limit int, matchMode string) ([]SearchHit, error) {
	tokens := strings.Fields(strings.TrimSpace(query))
	if len(tokens) == 0 {
		return nil, nil
	}

	tokenClauses := make([]string, 0, len(tokens))
	args := make([]any, 0, len(tokens)*2)
	for _, tok := range tokens {
		pattern := "%" + escapeLike(tok) + "%"
		tokenClauses = append(tokenClauses, "(name LIKE ? ESCAPE '\\' OR file_path LIKE ? ESCAPE '\\')")
		args = append(args, pattern, pattern)
	}

	joiner := " AND "
	if matchMode == "or" {
		joiner = " OR "
	}

	sql := fmt.Sprintf(`
		SELECT file_path, name, kind, start_line, end_line, scope
		FROM symbols
		WHERE %s
		ORDER BY file_path, start_line
		LIMIT ?
	`, strings.Join(tokenClauses, joiner))
	args = append(args, limit)

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("like search: %w", err)
	}
	defer rows.Close()
	return scanSearchHits(rows)
}

func scanSearchHits(rows *sql.Rows) ([]SearchHit, error) {
	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.FilePath, &h.Symbol, &h.Kind, &h.StartLine, &h.EndLine, &h.Scope); err != nil {
			return nil, fmt.Errorf("scan search hit: %w", err)
		}
		h.SymbolID = FormatSymbolID(SymbolRecord{
			FilePath:  h.FilePath,
			Kind:      h.Kind,
			Name:      h.Symbol,
			StartLine: h.StartLine,
		})
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search hits: %w", err)
	}
	return hits, nil
}

// GetSymbolByID looks up a symbol by its stable symbol_id string.
func (s *Store) GetSymbolByID(id string) (SymbolRecord, error) {
	key, err := ParseSymbolID(id)
	if err != nil {
		return SymbolRecord{}, err
	}

	var rec SymbolRecord
	var startByte, endByte sql.NullInt64
	err = s.db.QueryRow(`
		SELECT name, file_path, kind, scope, start_line, end_line, start_byte, end_byte
		FROM symbols
		WHERE file_path = ? AND kind = ? AND name = ? AND start_line = ?
	`, key.FilePath, key.Kind, key.Name, key.StartLine).Scan(
		&rec.Name, &rec.FilePath, &rec.Kind, &rec.Scope, &rec.StartLine, &rec.EndLine, &startByte, &endByte,
	)
	if err == sql.ErrNoRows {
		return SymbolRecord{}, fmt.Errorf("%w: %s", ErrSymbolNotFound, id)
	}
	if err != nil {
		return SymbolRecord{}, fmt.Errorf("get symbol by id: %w", err)
	}
	rec.StartByte = nullByteToInt(startByte)
	rec.EndByte = nullByteToInt(endByte)
	return rec, nil
}

// ListSymbolsByFile returns all symbols in filePath ordered by start_line.
func (s *Store) ListSymbolsByFile(filePath string) ([]SymbolRecord, error) {
	rows, err := s.db.Query(`
		SELECT name, file_path, kind, scope, start_line, end_line, start_byte, end_byte
		FROM symbols
		WHERE file_path = ?
		ORDER BY start_line
	`, filePath)
	if err != nil {
		return nil, fmt.Errorf("list symbols for %s: %w", filePath, err)
	}
	defer rows.Close()

	var symbols []SymbolRecord
	for rows.Next() {
		var rec SymbolRecord
		var startByte, endByte sql.NullInt64
		if err := rows.Scan(&rec.Name, &rec.FilePath, &rec.Kind, &rec.Scope, &rec.StartLine, &rec.EndLine, &startByte, &endByte); err != nil {
			return nil, fmt.Errorf("scan symbol row: %w", err)
		}
		rec.StartByte = nullByteToInt(startByte)
		rec.EndByte = nullByteToInt(endByte)
		symbols = append(symbols, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate symbols for %s: %w", filePath, err)
	}
	return symbols, nil
}

// SanitizeFTSQuery wraps each whitespace-separated token in double quotes for FTS5 MATCH.
// matchMode is "and" (implicit AND) or "or" (OR between tokens).
func SanitizeFTSQuery(query string, matchMode string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return `""`
	}

	tokens := strings.Fields(query)
	quoted := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.ReplaceAll(tok, `"`, `""`)
		quoted = append(quoted, `"`+tok+`"`)
	}

	sep := " "
	if normalizeMatchMode(matchMode) == "or" {
		sep = " OR "
	}
	return strings.Join(quoted, sep)
}

func annotateMatchedIn(query string, hits []SearchHit) {
	tokens := queryTokens(query)
	for i := range hits {
		hits[i].MatchedIn = inferMatchedIn(tokens, hits[i])
	}
}

func queryTokens(query string) []string {
	return strings.Fields(strings.TrimSpace(query))
}

func inferMatchedIn(tokens []string, hit SearchHit) string {
	if len(tokens) == 0 {
		return "symbol"
	}
	checks := []struct {
		field string
		name  string
	}{
		{hit.Symbol, "symbol"},
		{hit.FilePath, "path"},
		{hit.Kind, "kind"},
		{hit.Scope, "scope"},
	}
	for _, check := range checks {
		fieldLower := strings.ToLower(check.field)
		for _, tok := range tokens {
			if strings.Contains(fieldLower, strings.ToLower(tok)) {
				return check.name
			}
		}
	}
	return "symbol"
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func intToNullByte(v int) sql.NullInt64 {
	if v < 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

func nullByteToInt(v sql.NullInt64) int {
	if !v.Valid {
		return -1
	}
	return int(v.Int64)
}

func symbolByteColumns(sym SymbolRecord) (sql.NullInt64, sql.NullInt64) {
	if sym.StartByte < 0 || sym.EndByte <= sym.StartByte {
		return sql.NullInt64{}, sql.NullInt64{}
	}
	return intToNullByte(sym.StartByte), intToNullByte(sym.EndByte)
}
