# Litos MCP

A local MCP server that helps coding agents find code with fewer tokens. Instead of grepping the repo or reading whole files, agents search a **structural skeleton index** (symbols, kinds, scopes, line ranges) and read only the slices they need.

## What is this?

Litos MCP indexes your repository into a small SQLite database (`.lcn_cache.db`) at the project root. It extracts definitions—functions, types, classes, interfaces—not full source bodies. Agents query that index through MCP tools, get back compact JSON hits (~10 results by default), then pull targeted line ranges.

It runs as a **single Go binary** over stdio inside Cursor, Claude Code, and other MCP hosts. Everything stays on your machine: no cloud API, no embeddings service, no telemetry.

## Why use it?

- **Fewer tokens** — Search returns paths and line numbers, not matching lines or entire files. Reads are capped (500 lines / 512KB per call).
- **Faster discovery** — FTS5 keyword search over symbols beats scrolling large grep dumps for “where is X implemented?”
- **Stays fresh** — On `serve`, the index hydrates at startup, watches the repo for saves (`fsnotify`), and rechecks staleness before search. Normal edits do not require manual reindex.
- **Private and offline** — Local cache only; respects `.gitignore` and common skip dirs (`node_modules`, `.git`, build outputs).
- **Optional depth** — Install [Universal Ctags](https://github.com/universal-ctags/ctags) for broad multi-language symbol extraction, or use the built-in regex indexer for Go, TS/JS, and Python. Build with **`-tags treesitter`** (requires CGO) for byte-precise symbol boundaries on those same extensions.

## How it works

1. **`litos-mcp init`** (or auto-init on first `serve`) crawls indexable source files, extracts symbols, and writes `.lcn_cache.db` with an FTS5 search index.
2. **`litos-mcp serve`** starts the MCP server, hydrates any drift since last run, and watches the filesystem for changes while you work.
3. The agent **discovers** symbols via keyword search, exact name lookup, or a single-file outline.
4. The agent **fetches** source with **`read_symbol`** using the stable `symbol_id` from search or outline hits (`read_file_lines` remains a fallback).

Example search hit (no source body—just structure):

```json
[{
  "symbol_id": "src/billing/billing.go#function#ProcessPayment#56",
  "file_path": "src/billing/billing.go",
  "symbol": "ProcessPayment",
  "kind": "function",
  "start_line": 56,
  "end_line": 69,
  "scope": "BillingService",
  "matched_in": "symbol"
}]
```

Each hit includes a **`symbol_id`** (`file_path#kind#name#start_line`) that stays stable while `start_line`, `kind`, and `name` are unchanged. Symbol names must not contain `#`. Edits that move the definition or change only `end_line` may leave a stale ID or line range — re-search or re-outline after substantive edits.

After large tree changes (`git pull`, branch switch), call **`reindex_index`** or run **`litos-mcp init`** again. Check sync state anytime via the **`litos://index/status`** MCP resource.

## What it is not

- **Not semantic / embedding search** — Keyword and structural FTS over symbol metadata, not “find code similar to this paragraph.”
- **Not a grep replacement** — Use grep when you need literal matches in comments, strings, configs, or exact regex patterns litos does not index.
- **Not a hosted service** — You install the binary and wire it into Cursor, Claude Code, or another MCP host.

## Install

```bash
go install github.com/stubbies/litos-mcp/cmd/litos-mcp@latest
```

Or build from source:

```bash
git clone https://github.com/stubbies/litos-mcp.git
cd litos-mcp
go build -o bin/litos-mcp ./cmd/litos-mcp
```

Optional: install Universal Ctags for richer symbol extraction across many languages. Without it, litos-mcp falls back to built-in regex heuristics for **Go, TypeScript/JavaScript, and Python only** (`.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.py`, `.pyw`). Run `litos-mcp version` to see which indexer your install will use (`indexer: ctags available …` vs `regex fallback`) and whether byte-precise boundaries are active (`boundary: tree-sitter (go, ts, py)` vs `boundary: line-range only`).

### Byte-precise boundaries (optional)

By default, `read_symbol` returns a **line-range slice** derived from the primary indexer (ctags or regex). For exact definition spans on Go, TS/JS, and Python, build with tree-sitter:

```bash
CGO_ENABLED=1 go build -tags treesitter -o bin/litos-mcp ./cmd/litos-mcp
```

Tree-sitter refines symbol `start_byte` / `end_byte` at index time; `read_symbol` prefers those bytes when present. This requires CGO and a C toolchain. After upgrading to a build with byte columns, **delete `.lcn_cache.db`** and run `litos-mcp init` — existing caches are not migrated automatically.

### Language support

| Mode | Indexed extensions |
|------|-------------------|
| **Regex fallback** (no ctags) | `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.py`, `.pyw` |
| **Universal Ctags** | All crawl-eligible source extensions (Go, TS/JS, Python, Rust, Java/Kotlin, C/C++, C#, Ruby, PHP, Swift, Scala, Vue, Svelte, shell, SQL, Protobuf, and more) |

Crawl discovers many file types, but the regex indexer only extracts symbols from the extensions in the first row. With ctags installed, the full crawl set is indexed.

## Quick start

1. **Index your repo** (creates `.lcn_cache.db` beside the repo root):

   ```bash
   cd /path/to/your/project
   litos-mcp init
   ```

   Example output:

   ```
   files=842 symbols=3912 indexer=regex elapsed_ms=1204 db_bytes=524288
   ```

2. **Wire into your MCP host**

   #### Cursor

   Copy `.cursor/mcp.json` (or merge into your existing MCP config):

   ```json
   {
     "mcpServers": {
       "litos-mcp": {
         "command": "/path/to/litos-mcp",
         "args": ["serve"],
         "cwd": "${workspaceFolder}"
       }
     }
   }
   ```

   `cwd` must be the workspace root so the server resolves the same repo root as `init`.

   #### Claude Code

   Create **`.mcp.json`** in the **project you want indexed** (your app repo, not necessarily litos-mcp itself). Claude Code ignores `cwd` in MCP config; litos uses **`CLAUDE_PROJECT_DIR`** automatically.

   ```json
   {
     "mcpServers": {
       "litos-mcp": {
         "type": "stdio",
         "command": "litos-mcp",
         "args": ["serve"]
       }
     }
   }
   ```

   Use `litos-mcp` on your `PATH`, or substitute an absolute path (e.g. `$(go env GOPATH)/bin/litos-mcp` or `./bin/litos-mcp`).

   Alternative CLI:

   ```bash
   claude mcp add --scope project litos-mcp -- litos-mcp serve
   ```

   Restart Claude Code after config changes. Verify with **`/mcp`** or `claude mcp list`. First-time project-scoped servers may require approval in the UI.

3. **Use the MCP tools** in agent sessions:
   - `search_code_skeleton` — find symbols by keyword (default FTS) or by name (`name_match`: `exact` or `contains`); returns JSON hits with `symbol_id`, line ranges, and `matched_in`; optional `match_mode`: `and` or `or` (FTS only)
   - `outline_file` — list all indexed symbols in one file with `symbol_id`, kind, scope, and line ranges
   - `read_symbol` — read a bounded, line-numbered slice by `symbol_id` (preferred fetch path)
   - `read_file_lines` — read a bounded slice by file path and line range (fallback when you lack a `symbol_id`)
   - `reindex_index` — full index rebuild after large changes (e.g. `git pull`); normal saves sync automatically
   - **`litos://index/status`** (MCP resource) — JSON sync status: file/symbol counts, indexer, `boundary_indexer` (`treesitter` or `none`), `reconcile_needed`
   - **`code_discovery_workflow`** (MCP prompt) — onboarding text for agents: discover → fetch by `symbol_id`

`litos-mcp serve` auto-runs `init` when the cache is missing, then **hydrates** the index on startup (stat pass + boot crawl) and keeps it fresh with a **filesystem watcher** for debounced per-file updates. Search calls run a lightweight staleness check before FTS.

## Commands

| Command | Description |
|---------|-------------|
| `litos-mcp init [--root PATH]` | Build or refresh `.lcn_cache.db` |
| `litos-mcp serve` | MCP stdio server + boot hydration + fsnotify sync |
| `litos-mcp version` | Print binary, Go, indexer, boundary mode, and FTS5 status |

## Agent workflow

1. **Keyword discovery** — `search_code_skeleton(query="payment middleware")` for structural FTS hits (~10 results by default).
2. **Known symbol name** — `search_code_skeleton(query="ProcessPayment", name_match="exact")` for a case-sensitive name match (use `name_match="contains"` for substring).
3. **Known file** — `outline_file(file_path="src/billing/billing.go")` to list symbols and pick a `symbol_id`.
4. **Fetch** — `read_symbol(symbol_id=...)` using the ID from search or outline (preferred). Use `read_file_lines` only when you lack a `symbol_id`.
5. Prefer litos tools over grep or reading entire files when litos-mcp is available.

The MCP server exposes a **`code_discovery_workflow`** prompt with the same guidance. In Cursor, invoke it from the MCP prompts picker or reference it when onboarding agents to the repo.

### Claude Code (recommended)

Add **`CLAUDE.md`** at the project root with the same discovery workflow (discover → `read_symbol` → grep fallback). The MCP prompt **`code_discovery_workflow`** is available in Claude Code when the server is connected.

After edits, wait for fsnotify debounce (~300ms) or run a second search; use **`reindex_index`** after `git pull`. If `read_symbol` reports a stale ID, re-search or re-outline to get a fresh `symbol_id`.

### Cursor rule (recommended)

This repo includes `.cursor/rules/litos-code-discovery.mdc` as a template. Copy or adapt it in projects where litos-mcp is wired in:

```markdown
---
description: Prefer litos-mcp structural search over grep and whole-file reads
alwaysApply: true
---

# Code discovery with litos-mcp

When litos-mcp is connected (search_code_skeleton, outline_file, read_symbol, read_file_lines):

1. Keyword discovery — search_code_skeleton with functional keywords from the task.
2. Known symbol name — search_code_skeleton with name_match "exact" (case-sensitive) or "contains".
3. Known file — outline_file to list symbols and symbol_ids in that file.
4. Fetch — read_symbol(symbol_id) from search or outline hits; read_file_lines only as fallback.
5. Do not use grep, ripgrep, or Read on entire source files for discovery when litos tools are available.
6. Use grep or full-file reads only when litos returns no hits or you need exact regex/string matches.

Refine search queries using symbol names, kinds, scopes, matched_in, and symbol_id from prior hits. Re-search after edits if read_symbol reports a stale symbol_id.
```

## Cache and privacy

- **`.lcn_cache.db`** lives at the repository root, is gitignored, and can be deleted anytime; `init` or `serve` rebuilds it.
- Indexing skips paths covered by `.gitignore`, `.git/info/exclude`, and built-in skip rules (e.g. `node_modules`, `.git`, common build dirs).
- No source code is sent to external services—only your local agent reads files through MCP after you connect the server.

## Development

```bash
# Default CI: line-range reads, no CGO
CGO_ENABLED=0 go test ./...

# Tree-sitter CI: byte-precise boundaries (requires CGO + C toolchain)
CGO_ENABLED=1 go test -tags=treesitter ./...

# Search benchmark + regression guard (2× baseline in testdata/metrics.json)
go test -bench=BenchmarkFixtureSearch -benchtime=100ms ./internal/testutil/...

# Dogfood litos-mcp in this repo with Claude Code
go build -o ./litos-mcp ./cmd/litos-mcp
# Optional byte-precise build:
# CGO_ENABLED=1 go build -tags treesitter -o ./litos-mcp ./cmd/litos-mcp
# then point .mcp.json command at ./litos-mcp or $(pwd)/litos-mcp
```

GitHub Actions runs both jobs (default and `treesitter`) on push/PR — see `.github/workflows/ci.yml`.

Fixture repo: `testdata/fixture-repo/` with expectations in `testdata/metrics.json`.

## License

MIT — see [LICENSE](LICENSE).
