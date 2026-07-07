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
3. The agent **discovers** symbols via keyword search, exact name lookup, a single-file outline, or a **directory map** (definitions + outgoing calls under a prefix).
4. The agent **fetches** source with **`read_symbol`** using the stable `symbol_id` from search, outline, or map hits (`read_file_lines` remains a fallback).
5. The agent **traces callers** with **`find_callers`** by exact callee name (or `symbol_id`) to see who invokes a symbol, then reads caller bodies via `enclosing_symbol_id`.
6. The agent **validates edits** with **`check_file`** (tree-sitter build) to catch syntax errors before continuing.

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

**Call-site index:** Caller lookup adds a `call_sites` table indexed at sync time. If you upgrade from an older cache, run **`litos-mcp clean --reindex`** — existing caches are not migrated automatically (same as byte-boundary columns).

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

Optional: install Universal Ctags for richer symbol extraction across many languages. Without it, litos-mcp falls back to built-in regex heuristics for **Go, TypeScript/JavaScript, and Python only** (`.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.py`, `.pyw`). Run `litos-mcp version` to see which indexer your install will use (`indexer: ctags available …` vs `regex fallback`), whether byte-precise boundaries are active (`boundary: tree-sitter (go, ts, py)` vs `boundary: line-range only`), and how call sites are extracted (`callers: tree-sitter` vs `callers: regex`).

### Byte-precise boundaries (optional)

By default, `read_symbol` returns a **line-range slice** derived from the primary indexer (ctags or regex). For exact definition spans on Go, TS/JS, and Python, build with tree-sitter:

```bash
CGO_ENABLED=1 go build -tags treesitter -o bin/litos-mcp ./cmd/litos-mcp
```

Tree-sitter refines symbol `start_byte` / `end_byte` at index time; `read_symbol` prefers those bytes when present. This requires CGO and a C toolchain. After upgrading to a build with byte columns, run **`litos-mcp clean --reindex`** — existing caches are not migrated automatically.

### Language support

| Mode | Indexed extensions |
|------|-------------------|
| **Regex fallback** (no ctags) | `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.py`, `.pyw` |
| **Universal Ctags** | All crawl-eligible source extensions (Go, TS/JS, Python, Rust, Java/Kotlin, C/C++, C#, Ruby, PHP, Swift, Scala, Vue, Svelte, shell, SQL, Protobuf, and more) |

Crawl discovers many file types, but the regex indexer only extracts symbols from the extensions in the first row. With ctags installed, the full crawl set is indexed.

**Regex indexer scope:** On the regex fallback path, the indexer sets each symbol's `scope` from the immediately preceding `//` comment line (cleared after the next symbol match). This is a heuristic, not full ctags scope resolution — ctags and tree-sitter builds may differ. Doc comments above definitions improve FTS hits on the regex path; the eval locate task depends on scope-indexed doc text when running without ctags.

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
   - `find_callers` — find indexed call sites for a callee by **exact name** (case-sensitive) or `symbol_id`; returns file, line, column, and enclosing symbol metadata with `enclosing_symbol_id` for reading the caller
   - `map_directory` — directory architecture sketch: indexed definitions and outgoing calls under a repo-relative prefix (e.g. `src/handlers`), without reading file bodies; use when exploring a subsystem before drilling into symbols
   - `check_file` — post-edit syntax check for Go, TS/JS, and Python via tree-sitter; returns `{ "ok": true/false, "errors": [...] }`; requires a `-tags treesitter` build
   - `reindex_index` — full index rebuild after large changes (e.g. `git pull`); normal saves sync automatically
   - **`litos://index/status`** (MCP resource) — JSON sync status: file/symbol/`call_sites` counts, indexer, `boundary_indexer` (`treesitter` or `none`), `callers_indexer` (`treesitter` or `regex`), `reconcile_needed`
   - **`code_discovery_workflow`** (MCP prompt) — onboarding text for agents: map → discover → read → find callers → check edits → iterate

`litos-mcp serve` auto-runs `init` when the cache is missing, then **hydrates** the index on startup (stat pass + boot crawl) and keeps it fresh with a **filesystem watcher** for debounced per-file updates. Search calls run a lightweight staleness check before FTS.

## Commands

| Command | Description |
|---------|-------------|
| `litos-mcp init [--root PATH]` | Build or refresh `.lcn_cache.db` |
| `litos-mcp clean [--root PATH] [--reindex]` | Delete index cache (and optionally rebuild) |
| `litos-mcp serve` | MCP stdio server + boot hydration + fsnotify sync |
| `litos-mcp version` | Print binary, Go, indexer, boundary mode, callers mode, FTS5 status, and CLI subcommands |
| `litos-mcp search QUERY [--limit N] [--json]` | Keyword or name search (same engine as `search_code_skeleton`) |
| `litos-mcp outline FILE [--json]` | List symbols in one file |
| `litos-mcp read-symbol ID [--json]` | Read symbol slice by `symbol_id` (raw text without `--json`) |
| `litos-mcp find-callers NAME [--dir D] [--limit N] [--json]` | Caller lookup by exact name |
| `litos-mcp map-dir DIR [--json]` | Directory map: definitions + outgoing calls |
| `litos-mcp check FILE [--json]` | Syntax check (tree-sitter build only) |

Query subcommands accept `[--root PATH]` to override repo discovery. Use `--json` for machine-readable stdout; status messages go to stderr.

| Situation | Command |
|-----------|---------|
| Normal edits | automatic fsnotify sync |
| After `git pull` | `reindex_index` or `init` |
| Schema upgrade / corrupt cache / fresh start | `clean --reindex` |

## Agent workflow

1. **Subsystem sketch** — `map_directory(dir="src/handlers")` to see indexed definitions and outgoing calls in a package before reading bodies.
2. **Keyword discovery** — `search_code_skeleton(query="payment middleware")` for structural FTS hits (~10 results by default).
3. **Known symbol name** — `search_code_skeleton(query="ProcessPayment", name_match="exact")` for a case-sensitive name match (use `name_match="contains"` for substring).
4. **Known file** — `outline_file(file_path="src/billing/billing.go")` to list symbols and pick a `symbol_id`.
5. **Fetch** — `read_symbol(symbol_id=...)` using the ID from search, outline, or map hits (preferred). Use `read_file_lines` only when you lack a `symbol_id`.
6. **Find callers** — `find_callers(name=...)` or `find_callers(symbol_id=...)` to see who invokes a symbol; use `enclosing_symbol_id` from each hit to `read_symbol` on the caller and walk up the chain.
7. **Validate edits** — `check_file(file_path=...)` after editing Go, TS/JS, or Python source; fix syntax errors when `ok` is false (tree-sitter build required).
8. **CLI debugging** — `litos-mcp search "jwt verification" --json`, `litos-mcp map-dir src/handlers --json`, etc. mirror MCP tools from the terminal.
9. Prefer litos tools over grep or reading entire files when litos-mcp is available.

The MCP server exposes a **`code_discovery_workflow`** prompt with the same guidance. In Cursor, invoke it from the MCP prompts picker or reference it when onboarding agents to the repo.

### find_callers limitations

Call-site indexing is **name-based** (Grove-honest): `billing.ProcessPayment(...)` is indexed as callee `ProcessPayment`. There is no import-path, receiver, or type resolution.

- **Exact name only** — case-sensitive match on the callee identifier at the call site
- **Homonyms** — symbols with the same name in different packages may all appear in results; narrow with the optional `dir` prefix filter
- **Regex fallback** — without `-tags treesitter`, per-line regex may match false positives (e.g. function declarations that look like calls)
- **No macro/generated code** — only parsed/indexed source files are covered

If `find_callers` returns no hits, the indexed callee name may differ from what you expect — try `search_code_skeleton` first to confirm the symbol name.

### map_directory limitations

Directory maps are composed at query time from the `symbols` and `call_sites` tables — no separate index.

- **Outgoing calls only** — shows calls *made from* files under the directory prefix, not incoming references to symbols defined there
- **Name-based calls** — same Grove-honest callee names as `find_callers` (no import or type resolution)
- **Limits** — default 50 definitions and 50 outgoing call entries; raise with `def_limit` / `call_limit` when needed

### check_file limitations

Syntax checking requires a **`-tags treesitter`** build (CGO). Without it, the tool returns a clear error: *syntax check requires tree-sitter build.*

- **Supported extensions** — Go, TS/JS, and Python (same set as tree-sitter indexing)
- **Parse errors only** — reports tree-sitter `ERROR` nodes, not type-checker or linter diagnostics
- **Post-edit guard** — call after saving edits; does not replace `go test`, `tsc`, or your CI linter

### Claude Code (recommended)

Add **`CLAUDE.md`** at the project root with the same discovery workflow (map → discover → `read_symbol` → `find_callers` → `check_file` → grep fallback). The MCP prompt **`code_discovery_workflow`** is available in Claude Code when the server is connected.

After edits, call **`check_file`** on changed Go/TS/Python files when using a tree-sitter build; otherwise wait for fsnotify debounce (~300ms) or run a second search. Use **`reindex_index`** after `git pull`. If `read_symbol` reports a stale ID, re-search or re-outline to get a fresh `symbol_id`. If `find_callers` returns no hits, re-search to confirm the callee name.

### Cursor rule (recommended)

This repo includes `.cursor/rules/litos-code-discovery.mdc` as a template. Copy or adapt it in projects where litos-mcp is wired in:

```markdown
---
description: Prefer litos-mcp structural search over grep and whole-file reads
alwaysApply: true
---

# Code discovery with litos-mcp

When litos-mcp is connected (search_code_skeleton, outline_file, read_symbol, read_file_lines, find_callers, map_directory, check_file):

1. Subsystem sketch — map_directory with a repo-relative dir prefix when exploring a package before reading bodies.
2. Keyword discovery — search_code_skeleton with functional keywords from the task.
3. Known symbol name — search_code_skeleton with name_match "exact" (case-sensitive) or "contains".
4. Known file — outline_file to list symbols and symbol_ids in that file.
5. Fetch — read_symbol(symbol_id) from search, outline, or map hits; read_file_lines only as fallback.
6. Find callers — find_callers(name) or find_callers(symbol_id) after reading a symbol; use enclosing_symbol_id to read_symbol on callers and iterate.
7. Validate edits — check_file(file_path) after editing Go, TS/JS, or Python source when using a tree-sitter build.
8. Do not use grep, ripgrep, or Read on entire source files for discovery when litos tools are available.
9. Use grep or full-file reads only when litos returns no hits or you need exact regex/string matches.

Refine search queries using symbol names, kinds, scopes, matched_in, and symbol_id from prior hits. Re-search after edits if read_symbol reports a stale symbol_id. If find_callers returns no hits, try search_code_skeleton to confirm the callee name.
```

## Cache and privacy

- **`.lcn_cache.db`** lives at the repository root, is gitignored, and can be deleted anytime; `init`, `clean --reindex`, or `serve` rebuilds it. Use **`litos-mcp clean`** to remove the cache and WAL sidecars without manual `rm`.
- Indexing skips paths covered by `.gitignore`, `.git/info/exclude`, and built-in skip rules (e.g. `node_modules`, `.git`, common build dirs).
- No source code is sent to external services—only your local agent reads files through MCP after you connect the server.

## Development

```bash
# Default CI: line-range reads, no CGO
CGO_ENABLED=0 go test ./...

# Tree-sitter CI: byte-precise boundaries + check_file (requires CGO + C toolchain)
CGO_ENABLED=1 go test -tags=treesitter ./...

# Search benchmark + regression guard (2× baseline in testdata/metrics.json)
go test -bench=BenchmarkFixtureSearch -benchtime=100ms ./internal/testutil/...

# Eval task suite (locate, callers, map) + token budget guards
go test ./internal/eval/...

# Dogfood litos-mcp in this repo with Claude Code
go build -o ./litos-mcp ./cmd/litos-mcp
# Optional byte-precise build:
# CGO_ENABLED=1 go build -tags treesitter -o ./litos-mcp ./cmd/litos-mcp
# then point .mcp.json command at ./litos-mcp or $(pwd)/litos-mcp
```

GitHub Actions runs both jobs (default and `treesitter`) on push/PR — see `.github/workflows/ci.yml`.

Fixture repo: `testdata/fixture-repo/` with expectations in `testdata/metrics.json`.

### Eval harness

Lightweight fixture task suite inspired by [is-grep-enough](https://github.com/Entelligentsia/is-grep-enough): asserts that Litos query paths reach the right symbol, caller, or directory map with bounded token cost — not a full agent replay.

Tasks live in `testdata/eval/tasks.json`. `internal/eval/eval_test.go` runs each task via `internal/query` (not MCP stdio) and checks:

- **Locate** — search hits include the expected symbol and file; search output stays under `search_token_budget`; Litos tokens vs naive grep simulation stays under `grep_vs_litos_ratio_max` (0.5)
- **Trace callers** — `find_callers` hits include the expected enclosing symbol; output stays under `callers_token_budget`
- **Map directory** — `map_directory` meets minimum definition count and includes expected outgoing callee; output stays under `map_token_budget`

Token budgets and ratio thresholds are frozen in `testdata/metrics.json` under `thresholds`. Locate/callers/map eval tasks run in the default CI job; `check_file` tests run only in the tree-sitter job.

## License

MIT — see [LICENSE](LICENSE).
