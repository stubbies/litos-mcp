package store

const schemaDDL = `
CREATE TABLE IF NOT EXISTS files (
  path TEXT PRIMARY KEY,
  mtime_ns INTEGER NOT NULL,
  size INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS symbols (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  file_path TEXT NOT NULL,
  kind TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT '',
  start_line INTEGER NOT NULL,
  end_line INTEGER NOT NULL,
  start_byte INTEGER,
  end_byte INTEGER
);

CREATE INDEX IF NOT EXISTS idx_symbols_file_path ON symbols(file_path);

CREATE VIRTUAL TABLE IF NOT EXISTS symbols_fts USING fts5(
  name,
  file_path,
  kind,
  scope,
  content='symbols',
  content_rowid='id',
  tokenize='unicode61'
);

CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

const triggerDDL = `
DROP TRIGGER IF EXISTS symbols_ai;
CREATE TRIGGER symbols_ai AFTER INSERT ON symbols BEGIN
  INSERT INTO symbols_fts(rowid, name, file_path, kind, scope)
  VALUES (new.id, new.name, new.file_path, new.kind, COALESCE(new.scope, ''));
END;

DROP TRIGGER IF EXISTS symbols_ad;
CREATE TRIGGER symbols_ad AFTER DELETE ON symbols BEGIN
  INSERT INTO symbols_fts(symbols_fts, rowid, name, file_path, kind, scope)
  VALUES ('delete', old.id, old.name, old.file_path, old.kind, COALESCE(old.scope, ''));
END;

DROP TRIGGER IF EXISTS symbols_au;
CREATE TRIGGER symbols_au AFTER UPDATE ON symbols BEGIN
  INSERT INTO symbols_fts(symbols_fts, rowid, name, file_path, kind, scope)
  VALUES ('delete', old.id, old.name, old.file_path, old.kind, COALESCE(old.scope, ''));
  INSERT INTO symbols_fts(rowid, name, file_path, kind, scope)
  VALUES (new.id, new.name, new.file_path, new.kind, COALESCE(new.scope, ''));
END;
`
