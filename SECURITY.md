# Security and deployment notes

## Historical note (credentials)
Earlier commits in this repository's public git history contain a default
PostgreSQL credential from v1. Rewriting history does not undo that exposure.
As of v2 the service no longer uses PostgreSQL at all (the legacy `mem`
database was dumped and dropped on 2026-11-14), so nothing in this deployment
depends on that credential — but do not reuse it anywhere.

## Required environment
Configure `/etc/mem/mem.env` as root with mode `0600`:

```sh
sudo install -d -m 0750 /etc/mem
sudo install -m 0600 /dev/null /etc/mem/mem.env
sudoedit /etc/mem/mem.env
```

```ini
MEM_WIKI_DIR=/home/admin/mem-wiki
MEM_LISTEN_ADDR=127.0.0.1:3200
MEM_API_TOKEN=REPLACE_WITH_A_LONG_RANDOM_SECRET
MEM_UI_USER=admin
MEM_UI_PASSWORD=REPLACE_WITH_A_DIFFERENT_LONG_RANDOM_SECRET
```

Generate secrets with `openssl rand -hex 32`. `MEM_STEWARD_TOKEN` is still
accepted as an equivalent API token for older deployments, but v2 has a single
access level: any valid token (or the UI Basic login) can read and write.
There is no per-identity permission model — provenance (`X-Mem-Source`,
`bot_id`) is recorded but not enforced.

## Exposure
The service defaults to loopback-only and refuses to start with no credential
configured unless `MEM_ALLOW_UNAUTHENTICATED=true` (local development only).
A socket proxy or reverse proxy in front of a loopback listener makes every
remote client look local, so never combine that opt-out with a proxy. Plain
HTTP exposes tokens on the network: use the systemd socket proxy, TLS, or an
encrypted tunnel beyond the local host.

## Browser protections
The web UI is served with a per-response Content-Security-Policy nonce, no
external assets, `X-Frame-Options: DENY`, and `Referrer-Policy: no-referrer`.
API writes that carry a cross-site `Origin` header are rejected (add
legitimate proxy origins to `MEM_ALLOWED_ORIGINS`).

## Destructive operations
- There is no bulk-delete endpoint. Page deletion moves the file to `trash/`.
- The organizer hard-deletes trash only after `MEM_TRASH_DAYS` (default 30).
- `POST /v1/memory/restore` replaces page files from a backup archive; it is
  protected by API authentication. Treat any client holding the token as able
  to restore.

## Backup and restore
Backups are gzipped tar archives of the wiki directory (the SQLite index is
excluded — it is derived state):

```sh
umask 077
/home/admin/projects/mem/mem backup --file "/var/backups/mem-$(date +%F-%H%M%S).tar.gz"
```

Restore into a scratch directory before relying on a backup:
`mem restore` accepts a file path or stdin; verify with `mem list` afterwards.
Automate backups with the host scheduler, monitor failures, and periodically
test restore.

## Runtime changes

```sh
sudo systemctl daemon-reload
sudo systemctl restart memory.service
curl http://127.0.0.1:3200/health
```

## Remaining work
- Add integration tests against a disposable wiki directory.
- Add rate limiting and request-level audit logging at the reverse proxy.
- Consider per-agent tokens (one identity per token) with read-only levels;
  today every token holder has full read/write.
