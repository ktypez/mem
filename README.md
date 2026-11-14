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

The web UI at `/` is a single page: search, read, edit, create, trash.

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
