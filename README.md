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
- **Optional depth** — Install [Universal Ctags](https://github.com/universal-ctags/ctags) for broad multi-language symbol extraction, or use the built-in regex indexer for Go, TS/JS, and Python.

## How it works

1. **`litos-mcp init`** (or auto-init on first `serve`) crawls indexable source files, extracts symbols, and writes `.lcn_cache.db` with an FTS5 search index.
2. **`litos-mcp serve`** starts the MCP server, hydrates any drift since last run, and watches the filesystem for changes while you work.
3. The agent calls **`search_code_skeleton`** with keywords (e.g. `"jwt verification"`, `"payment handler"`).
4. For promising hits, the agent calls **`read_file_lines`** with the returned `file_path`, `start_line`, and `end_line`.

Example search hit (no source body—just structure):

```json
[{
  "file_path": "src/billing/billing.go",
  "symbol": "ProcessPayment",
  "kind": "function",
  "start_line": 56,
  "end_line": 69,
  "scope": "BillingService",
  "matched_in": "symbol"
}]
```

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

Optional: install Universal Ctags for richer symbol extraction across many languages. Without it, litos-mcp falls back to built-in regex heuristics for **Go, TypeScript/JavaScript, and Python only** (`.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.py`, `.pyw`). Run `litos-mcp version` to see which indexer your install will use (`indexer: ctags available …` vs `regex fallback`).

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
   - `search_code_skeleton` — find symbols by keyword (returns JSON hits with file path, line ranges, and `matched_in`; optional `match_mode`: `and` or `or`)
   - `read_file_lines` — read a bounded, line-numbered slice after search confirms the target
   - `reindex_index` — full index rebuild after large changes (e.g. `git pull`); normal saves sync automatically
   - **`litos://index/status`** (MCP resource) — JSON sync status: file/symbol counts, indexer, `reconcile_needed`
   - **`code_discovery_workflow`** (MCP prompt) — onboarding text for agents: search first, read narrowly

`litos-mcp serve` auto-runs `init` when the cache is missing, then **hydrates** the index on startup (stat pass + boot crawl) and keeps it fresh with a **filesystem watcher** for debounced per-file updates. Search calls run a lightweight staleness check before FTS.

## Commands

| Command | Description |
|---------|-------------|
| `litos-mcp init [--root PATH]` | Build or refresh `.lcn_cache.db` |
| `litos-mcp serve` | MCP stdio server + boot hydration + fsnotify sync |
| `litos-mcp version` | Print binary, Go, indexer, and FTS5 status |

## Agent workflow

1. Call `search_code_skeleton(query="payment middleware")` for structural hits (~10 results by default).
2. Call `read_file_lines(file_path, start_line, end_line)` only for confirmed ranges from search results.
3. Prefer these tools over grep or reading entire files when litos-mcp is available.

The MCP server exposes a **`code_discovery_workflow`** prompt with the same guidance. In Cursor, invoke it from the MCP prompts picker or reference it when onboarding agents to the repo.

### Claude Code (recommended)

Add **`CLAUDE.md`** at the project root with the same discovery workflow (search → read slices → grep fallback). The MCP prompt **`code_discovery_workflow`** is available in Claude Code when the server is connected.

After edits, wait for fsnotify debounce (~300ms) or run a second search; use **`reindex_index`** after `git pull`.

### Cursor rule (recommended)

Add `.cursor/rules/litos-code-discovery.mdc` so agents default to litos tools in this workspace:

```markdown
---
description: Prefer litos-mcp structural search over grep and whole-file reads
alwaysApply: true
---

# Code discovery with litos-mcp

When litos-mcp is connected (search_code_skeleton, read_file_lines):

1. Start with search_code_skeleton using functional keywords from the task.
2. Read only the line ranges returned by search via read_file_lines.
3. Do not use grep, ripgrep, or Read on entire source files for discovery when litos tools are available.
4. Use grep or full-file reads only as a fallback when litos returns no hits or you need exact regex matches.

Refine search queries using symbol names, kinds, scopes, and matched_in from prior hits.
```

## Cache and privacy

- **`.lcn_cache.db`** lives at the repository root, is gitignored, and can be deleted anytime; `init` or `serve` rebuilds it.
- Indexing skips paths covered by `.gitignore`, `.git/info/exclude`, and built-in skip rules (e.g. `node_modules`, `.git`, common build dirs).
- No source code is sent to external services—only your local agent reads files through MCP after you connect the server.

## Development

```bash
# Run tests (includes fixture golden metrics, token/size/latency thresholds)
go test ./...

# Search benchmark + regression guard (2× baseline in testdata/metrics.json)
go test -bench=BenchmarkFixtureSearch -benchtime=100ms ./internal/testutil/...

# Dogfood litos-mcp in this repo with Claude Code
go build -o ./litos-mcp ./cmd/litos-mcp
# then point .mcp.json command at ./litos-mcp or $(pwd)/litos-mcp
```

Fixture repo: `testdata/fixture-repo/` with expectations in `testdata/metrics.json`.

## License

MIT — see [LICENSE](LICENSE).
