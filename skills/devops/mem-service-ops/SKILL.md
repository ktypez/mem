---
name: mem-service-ops
description: Use when deploying or verifying the mem service.
version: 2.0.0
author: mem contributors
license: MIT
platforms: [linux]
metadata:
  hermes:
    tags: [mem, wiki, go, sqlite, systemd, memory]
    related_skills: []
---

# mem service operations (v2 — wiki mode)

mem is a wiki memory service: markdown pages on disk (`MEM_WIKI_DIR`, default
`~/mem-wiki`) plus a derived SQLite FTS5 index. Full specification:
[`docs/SPEC.md`](../../../docs/SPEC.md) in the repo at
`/home/admin/projects/mem`.

## Deploy / restart

```sh
cd /home/admin/projects/mem
go test ./... && go build -o mem .
sudo systemctl restart memory.service
curl -s http://127.0.0.1:3200/health        # {"mode":"wiki",...}
```

Unit: `memory.service` (env `/etc/mem/mem.env`). LAN entry:
`mem-lan.socket` (socket-activated proxy to loopback).

## Verify

```sh
curl -s http://127.0.0.1:3200/health
mem list                                    # pages exist
mem search "anything" -limit 3              # FTS + LIKE fallback works
systemctl list-timers mem-organize.timer    # nightly organizer scheduled
```

## Housekeeping

```sh
mem organize                                # dedupe, trash rotation, index optimize
mem backup --file backup.tar.gz             # gzipped tar of the wiki dir
mem restore --file backup.tar.gz            # restore + rescan
mem reindex                                 # rebuild index from files
```

## Gotchas

- `salt`, `mem`, `pi-bolt` live in `~/.local/bin` — use full paths or export PATH.
- Index `.mem-index.db*` is derived: never back it up, always `mem reindex` after
  bulk hand edits.
- Legacy v1 (Postgres graph) import: `mem import-legacy --file <old.json.gz>`.
