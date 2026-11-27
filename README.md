# mem

`mem` is a self-hosted **wiki-style memory service** for AI agents. Pages are
markdown files on disk (`MEM_WIKI_DIR`, default `~/mem-wiki`); a SQLite FTS5
index is derived from them and can be rebuilt at any time. Every agent can read
and write: a fact is appended to the page for its topic immediately, with the
writer recorded as provenance.

## Design

- **Markdown files are the source of truth.** Each page is `<slug>.md` with a
  small frontmatter header (title, tags, created, updated, updated_by).
- **Facts are appended, never buried.** `POST /v1/memory/nodes` or
  `/v1/memory/extract` writes a `- <fact> — <source>, <date>` line under the
  page's `## Facts` section. Duplicate bodies update the page instead of piling up.
- **No review gate.** There is no proposed inbox: writes are active at once.
- **Nothing is erased.** Deleting a page moves it to `trash/`.
- **Derived index.** SQLite FTS5 (trigram tokenizer, so Thai and other
  unspaced languages match by substring) with a LIKE fallback for short
  queries. `mem reindex` rebuilds it from the files; the server also rescans
  every 5 minutes so hand edits are picked up.
- **No external dependencies.** No PostgreSQL, no embedding endpoint, no
  vector database. A single static Go binary.

## API

Wiki API:

- `GET  /v1/wiki/pages` — list pages
- `POST /v1/wiki/pages` — create/upsert `{title, body, tags}`
- `GET|PUT|DELETE /v1/wiki/pages/{slug}` — read, replace, trash
- `POST /v1/wiki/reindex` — rescan the directory into the index

Compatibility API (existing clients keep working):

- `GET|POST /v1/memory/nodes` — list pages as nodes / append a fact
- `GET|POST /v1/memory/search`, `/v1/memory/context` — keyword/trigram search
- `POST /v1/memory/extract` — append a fact
- `GET /v1/memory/stats`, `/v1/memory/brief`, `/v1/memory/inbox` (always empty)
- `GET /v1/memory/backup`, `POST /v1/memory/restore` — gzipped tar archive
- `POST /v1/memory/nodes/{id}/promote|reject` — accepted, answers `noop`

Add `format=text` to `brief`, `search`, `stats` and `/v1/wiki/pages/{slug}` for a
token-cheap plain-text shape: `brief` and `search` print one compact line per
page/hit under a header line (`mem index: N of M pages`, `mem search: N hit(s)`),
`stats` prints a single summary line, and a page renders as markdown. JSON stays
the default, so existing clients are unaffected.

`scripts/mem-agents-index.py` writes that same index into a managed block
(`<!-- mem-index:start -->` … `<!-- mem-index:end -->`) of a shared instruction
file — by default `~/AGENTS.md` — so agent runtimes without a memory hook
(opencode, pi, pi-bolt) start a session knowing what the store holds. It is
idempotent, replaces the file atomically, and leaves it untouched when the
service is unreachable. Pair it with the units `mem-agents-index.service` and
`mem-agents-index.timer` (user units, every 10 minutes):

```
install -m 0755 scripts/mem-agents-index.py ~/.local/bin/
install -m 0644 mem-agents-index.service mem-agents-index.timer ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now mem-agents-index.timer
```

`scripts/mem-mcp.py` is the stdio MCP bridge for MCP-capable runtimes, including
Codex, Antigravity, OpenCode, and pi-bolt. Install the copy where the runtime
spawns it (`~/.local/bin/mem-mcp.py`) and keep it in sync with the repository
copy. Each runtime must configure the same bridge; the shared `AGENTS.md` index
does not provide read/write access by itself.

The web UI at `/` is a single page in the local "mochi pastel" style shared
with DATA Ledger / Truck — paper `#FFFDF8` with a dot grid, violet ink,
pastel pill fills, soft sticker shadows, and four palettes (`mochi`, `matcha`,
`soda`, `sakura`) plus auto / light / dark mode. It provides search, read,
edit, create, and trash, with the page list in an off-canvas drawer on a phone.
Itim / Mali / Prompt webfonts are embedded in the binary and served from
`/ui/fonts`, so there are still no external assets; it works with Basic auth.

## CLI

```sh
mem                               # interactive TUI (browse, search, create pages)
mem list                          # pages; styled table on a terminal
mem show PAGE                     # page; rendered markdown on a terminal
mem search QUERY                  # full-text search; plain when piped
mem add PAGE "fact text"          # append a fact line to a page's ## Facts
mem server                        # start the HTTP server
mem backup --file backup.tar.gz   # gzipped tar of the wiki directory
mem restore --file backup.tar.gz
mem reindex                       # rebuild the search index from the files
mem import-legacy --file old.json.gz  # convert a v1 nodes+edges backup into pages
```

Output is styled only when stdout is a terminal; piped output (agents,
scripts) is plain text exactly as before, and `-json` / `-plain` force
machine-readable or unstyled output on any command.


## Configuration

```ini
MEM_WIKI_DIR=/home/admin/mem-wiki   # where the pages live
MEM_LISTEN_ADDR=127.0.0.1:3200      # loopback by default
MEM_API_TOKEN=...                   # Bearer token for agents (read + write)
MEM_UI_USER=admin
MEM_UI_PASSWORD=...                 # web UI basic auth
MEM_ALLOWED_ORIGINS=...             # extra browser origins behind reverse proxies
```

`MEM_STEWARD_TOKEN` is still accepted as an equivalent API token for older
deployments. A non-loopback bind without any token refuses to start.

## History

v1 stored nodes and relationships in PostgreSQL with a steward review inbox and
optional pgvector semantic search. It is preserved on the `v1` tag and the
`mem.bak-v1-*` binaries; `mem import-legacy` migrates its backups.
