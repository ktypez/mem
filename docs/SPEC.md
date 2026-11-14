# mem — Specification (v2, wiki mode)

Status: implemented and deployed on the lab host as of 2026-11-14.
Source of truth for behaviour alongside the code: `wiki.go`, `index.go`,
`server.go`, `auth.go`, `backup.go`, `organizer.go`, `migrate.go`, `main.go`.

## 1. Model

A wiki-style memory service for AI agents.

- **Markdown pages on disk are the source of truth.** One page = one file
  `<slug>.md` in `MEM_WIKI_DIR` (default `~/mem-wiki`).
- **The search index is derived state.** SQLite FTS5 at
  `<wiki>/.mem-index.db`, always rebuildable from the files.
- **Open writes.** Any authenticated client may read and write. There is no
  review inbox; the writer label is stored as provenance only.
- **Append semantics.** New knowledge is appended as fact lines; nothing is
  silently overwritten.
- **Nothing is erased.** Page deletion moves the file into `<wiki>/trash/`.

## 2. Page format

```markdown
---
title: Truck
tags: [memory, migrated]
created: 2026-11-14T13:28:54+07:00
updated: 2026-11-14T13:35:10+07:00
updated_by: opencode
---

Free-form markdown body.

## Facts
- battery at 64% — opencode, 2026-11-14
```

- Frontmatter keys: `title`, `tags`, `created`, `updated`, `updated_by`
  (timestamps RFC 3339). Malformed headers do not break a page: the file stem
  becomes the title and the whole content becomes the body.
- Fact lines: `- <body> — <source>, <YYYY-MM-DD>` under `## Facts` (heading
  created on demand). A fact whose body already appears on the page is a
  no-op (the page is touched, not duplicated).
- Slugs: keep Unicode letters/digits/marks (`\p{L}\p{N}\p{M}`), separators and
  unsafe characters collapse to `-`, max 80 runes, collisions get `-2`, `-3`, …
- `trash/` files are named `<slug>.<timestamp>.md`.
- Index files (`.mem-index.db*`, `*.tmp`) are never part of backups.

## 3. Index

- SQLite via `modernc.org/sqlite` (pure Go, embedded), WAL mode,
  `busy_timeout=5000`.
- `pages` table: slug, title, tags, body, created/updated/updated_by,
  mtime+size (for cheap rescan comparisons).
- `pages_fts`: FTS5, columns `slug (unindexed), title, body, tags`,
  tokenizer **trigram** — so Thai and other unspaced languages match by
  substring.
- Search: queries ≥ 3 characters run as FTS `MATCH` (every whitespace term
  double-quoted, so punctuation cannot alter the query); shorter queries and
  zero-hit results fall back to a `LIKE` substring scan.
- `excerpt()` builds a display window centred on the first match.
- Sync points: server startup, a 5-minute rescan loop, `POST /v1/wiki/reindex`,
  `mem reindex`, after restore, before import.

## 4. HTTP API

Binding: `MEM_LISTEN_ADDR` (default `127.0.0.1:3200`). `/health` is always
open. Everything under `/v1/` and `/api/` requires a Bearer token
(`MEM_API_TOKEN`; `MEM_STEWARD_TOKEN` accepted as an equivalent). The web UI
requires Basic auth. A non-loopback bind with no token refuses to start;
running without any credential requires `MEM_ALLOW_UNAUTHENTICATED=true`
(loopback development only). Cross-origin browser writes are rejected unless
the origin is in `MEM_ALLOWED_ORIGINS`. Request bodies capped at 1 MB
(512 MB for restore).

### Wiki API
| Method & path | Behaviour |
|---|---|
| `GET /v1/wiki/pages` | list pages (frontmatter + 160-char body preview) |
| `POST /v1/wiki/pages` | create/upsert `{title?, slug?, body, tags}`; no slug → slug from title (case-insensitive title match reuses the existing page) |
| `GET /v1/wiki/pages/{slug}` | full page |
| `PUT /v1/wiki/pages/{slug}` | replace body / title / tags |
| `DELETE /v1/wiki/pages/{slug}` | move to `trash/` |
| `POST /v1/wiki/reindex` | rescan the directory into the index |

### Compatibility API (kept for existing clients)
| Method & path | Behaviour |
|---|---|
| `GET /v1/memory/nodes` | pages listed node-shaped |
| `POST /v1/memory/nodes` | `{body, topic?, subject?, source?/bot_id?}` → append a fact to the topic page (fallbacks: subject, `Inbox`) |
| `GET/DELETE /v1/memory/nodes/{id}` | page read / trash (id = slug) |
| `POST /v1/memory/nodes/{id}/promote` or `/reject` | accepted, answers `noop` (no inbox exists) |
| `GET /v1/memory/edges` | always empty |
| `GET/POST /v1/memory/search` | `{query, limit}` → node-shaped hits (`semantic: false`) |
| `GET/POST /v1/memory/context` | joined excerpts `[Title] …` |
| `POST /v1/memory/extract` | append a fact |
| `GET /v1/memory/stats` | `{pages, nodes (=pages), edges: 0, layers: {}, status: {active: n}}` |
| `GET /v1/memory/brief` | recently updated pages with previews |
| `GET /v1/memory/inbox` | always empty |
| `GET /v1/memory/backup` | stream a gzipped tar of the wiki dir |
| `POST /v1/memory/restore` | extract a backup, then rescan |

Aliases: `/api/nodes`, `/api/nodes/…`, `/api/brief`, `/api/inbox`.

### Web UI
Single page at `/` (CSP with per-response nonce, no external assets):
page list, full-text search, read (minimal markdown render), edit, create,
trash. Auth: Basic.

## 5. CLI

```
mem                               # interactive TUI (terminal only)
mem server                        # HTTP service
mem list [-json]                  # pages (title, slug, updated, tags)
mem show PAGE [-plain]            # page; Glamour-rendered markdown on a terminal
mem search QUERY [-limit N] [-json]
mem add PAGE TEXT [-source S]     # append a fact line (default source: cli)
mem organize                      # nightly housekeeping (see §6)
mem backup --file F [--gzip]      # gzipped tar of the wiki dir
mem restore --file F              # extract a backup, rescan
mem reindex                       # rebuild the index from the files
mem import-legacy --file F        # v1 backup → pages
```

`add`/`search` accept flags anywhere (arguments are reordered internally;
Go's flag package otherwise stops at the first positional).

### Output modes

Output depends on context, in priority order:

1. `-json` — machine-readable JSON, always unstyled (agents/pipes).
2. `-plain` — plain text even on a terminal.
3. Terminal (stdout is a TTY) — styled rendering: Lip Gloss table for
   `list`, Glamour markdown for `show`, colored hits for `search`.
4. Pipe (default for scripts/agents) — plain text, unchanged from v2.0
   pre-ricing behavior; scripts written against earlier output keep working.

The TUI (bare `mem` on a terminal) is browse-only plus page creation:
bubbles/list sidebar with live FTS search (`/`), Glamour preview and
scrollable reader (enter), page creation (`n`), index resync (`r`),
quit (`q`). It uses the same index as the CLI/server; nothing else.


## 6. Organizer

`mem organize` (systemd timer `mem-organize.timer`, daily 03:30 local,
`Persistent=true`):

1. rescan the index against the files
2. within a page, drop exact duplicate fact lines (auto)
3. collect facts whose normalized body appears on more than one page —
   **reported only**, appended to the `mem-organizer` page (a script must not
   decide which page a fact belongs to)
4. hard-delete `trash/` files older than `MEM_TRASH_DAYS` (default 30)
5. `PRAGMA optimize` on the index
6. append a run summary to the `mem-organizer` page

## 7. Configuration

| Variable | Default | Meaning |
|---|---|---|
| `MEM_WIKI_DIR` | `~/mem-wiki` | page directory |
| `MEM_LISTEN_ADDR` | `127.0.0.1:3200` | bind address |
| `MEM_API_TOKEN` | — | Bearer token for agents (read + write) |
| `MEM_STEWARD_TOKEN` | — | accepted as an equivalent token (legacy) |
| `MEM_UI_USER` | `admin` | web UI basic-auth user |
| `MEM_UI_PASSWORD` | — | web UI basic-auth password |
| `MEM_ALLOWED_ORIGINS` | — | extra browser origins behind reverse proxies |
| `MEM_ALLOW_UNAUTHENTICATED` | — | `true` allows credential-less loopback runs |
| `MEM_TRASH_DAYS` | `30` | organizer trash retention |

Deployment on the lab host: systemd `memory.service`
(`/home/admin/projects/mem/mem server`, env `/etc/mem/mem.env`) and
`mem-lan.socket` (LAN `192.168.1.47:3200` → socket-proxy → loopback).

## 8. Clients

- **mem-mcp.py** (`~/.local/bin/mem-mcp.py`) — stdio MCP server fronting the
  REST API; wired into opencode and pi-bolt (`~/.pi/agent/mcp.json`).
  Tools: `memory_search`, `memory_add`, `memory_context`, `memory_extract`,
  `memory_stats`, `memory_inbox` (always empty), `memory_promote`/`reject`
  (no-ops).
- **Hermes memory plugin** — REST writes land as fact lines.
- **CLI** and the **web UI**.
- Any agent with filesystem access can also read/append the `.md` files
  directly; the rescan loop picks up hand edits within 5 minutes (or via
  `mem reindex`).

## 9. Guarantees and limits

Guarantees
- Writes are durable: atomic tmp+rename, then index update; a failed index
  update never corrupts the file (and the index is rebuildable anyway).
- Backups exclude derived state; a restore always rebuilds the index.
- Deletes are reversible within the retention window.

Limits (accepted trade-offs)
- Fact dedupe is substring-based: a short fact that is a substring of another
  line on the same page is suppressed.
- Trigram search needs ≥ 3 characters; 1–2 character queries use the
  case-sensitive-for-non-ASCII LIKE fallback.
- No per-identity permissions: one token = full read/write (provenance only).
- Single host, single wiki namespace; there is no multi-agent memory
  separation by design (all agents share one wiki).
- The organizer never merges or rewrites page bodies beyond exact-duplicate
  lines; semantic consolidation is a human/agent task by design.

## 10. Legacy / rollback

- v1 (Postgres nodes + edges, steward inbox, pgvector, fact refresher) is
  preserved: pre-2026-11-14 commits, the `v1` tag, `mem.bak-v1-*` binaries,
  and the untouched Postgres database.
- `mem import-legacy --file backup.json.gz` groups v1 nodes by topic/subject
  into pages (system/retired facts skipped, pages tagged `migrated`).
- Reference snapshot: `~/mem-legacy-backup-20261114.json.gz`.
