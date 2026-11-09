# Deployment and operations

This guide covers running `mem` as a service, backup/restore, and rebuilding generated graph relationships. For environment security, see [SECURITY.md](../SECURITY.md).

## Configure and build

1. Provision PostgreSQL and a database role with only the privileges needed by the service (including table/index creation for startup initialization, or pre-provision schema and adjust privileges as appropriate).
2. Create a protected environment file. Do not commit it or print its contents:

   ```sh
   sudo install -d -m 0750 /etc/mem
   sudo install -m 0600 /dev/null /etc/mem/mem.env
   sudoedit /etc/mem/mem.env
   ```

   Configure `DATABASE_URL`, `MEM_LISTEN_ADDR=127.0.0.1:3200`, `MEM_API_TOKEN` (agents), `MEM_STEWARD_TOKEN` (steward role: active writes, inbox review, rebuild, restore), and, when the UI is exposed, `MEM_UI_USER` and `MEM_UI_PASSWORD`. Optionally set `MEM_STEWARD_SOURCE=steward,hermes` for steward writer labels and `MEM_ALLOWED_ORIGINS` when a reverse proxy rewrites the Host header. For optional semantic search, install the `vector` extension once as a superuser and set `MEM_EMBED_URL`, `MEM_EMBED_MODEL` and `MEM_EMBED_API_KEY`; leave them unset to keep keyword-only search. The service will not start without any credential unless `MEM_ALLOW_UNAUTHENTICATED=true` is set, which is for local development only. Keep credentials out of commands, logs, documentation, and repository files.
3. Build and test with the Go version in `go.mod`:

   ```sh
   go test ./...
   go vet ./...
   go build -o mem .
   ```

4. Install the binary and a service unit appropriate for the host. The example [`memory.service`](../memory.service) runs as the local `admin` account and reads `/etc/mem/mem.env`; adapt `User`, `WorkingDirectory`, and `ExecStart` rather than copying host-specific values blindly.
5. Start the service and verify availability:

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl enable --now memory.service
   systemctl is-active memory.service
   curl -fsS http://127.0.0.1:3200/health
   ```

   The health route confirms the service can query PostgreSQL. Check the authenticated stats endpoint and load `/graph` from an authorized browser as a further smoke test.
6. If an AI agent on this host should use the repository's operations skill, install it for the chosen runtime from the checked-out repository:

   ```sh
   ./scripts/install-agent-skill.sh
   ```

   The default `all` target installs to Codex/OpenCode's shared `~/.agents/skills`, Claude Code's `~/.claude/skills`, and Hermes' `$HERMES_HOME/skills/devops` (default `~/.hermes/skills/devops`). Select `--target codex|opencode|claude|hermes` to install just one. Alternatively, place the skill in the runtime's repository-local skill directory. The installer preflights destinations and refuses to replace a differing copy unless explicitly given `--replace`. This installs skill files only; it does not install or authenticate mem, configure MCP, or restart any service.

## Network exposure

Keep the Go service bound to loopback. Never bind to `0.0.0.0` as a shortcut. A proxy in front of a loopback listener makes every remote client look local, so credentials must be configured on the service itself. Plain HTTP over a LAN sends tokens and Basic credentials in cleartext; use the Tailscale Serve HTTPS endpoint for remote access instead. The host-specific `mem-lan.socket` / `mem-lan.service` forwards the Wi-Fi LAN address to the loopback listener so the dashboard card can reach the console from another device; the service still enforces bearer/Basic auth (an unauthenticated request gets 401). Enable it only when LAN access is wanted, and note that credentials travel unencrypted on that hop. The sample is not a generic internet-facing deployment recipe. `/health` does not require authentication, so restrict access even when API and UI authentication are configured.

## Backups and restore drills

The CLI uses `DATABASE_URL` and exports/imports the database as JSON:

```sh
umask 077
./mem backup --file /secure/backups/mem-$(date +%F-%H%M%S).json
./mem backup --file /secure/backups/mem-$(date +%F-%H%M%S).json.gz --gzip
```

Backups written by the CLI and by `GET /v1/memory/backup` use the same format and include every status, so they are interchangeable. CLI backup files are created with mode `0600`. The CLI restore operation imports a JSON backup (gzip is detected automatically) inside a single transaction, so a failed restore leaves the database unchanged; it does not perform a PostgreSQL schema snapshot or replace the `pg_dump` workflow. For full database recovery, use a protected PostgreSQL backup such as:

```sh
umask 077
pg_dump --format=custom --file="/secure/backups/mem-$(date +%F-%H%M%S).dump" "$PGDATABASE"
```

Keep backups off-host, enforce retention, monitor job failures, and periodically restore into a separate test database. Do not test restores against the production database. The HTTP restore API replaces all data, requires steward credentials, and must be sent with `X-Confirm-Restore: replace-all`.

## Rebuild generated graph edges

`POST /v1/memory/rebuild-edges` deletes and recreates only `same_topic` and `same_subject` relationships. Manually created relationships and other relation types remain. Rebuilding considers all nodes, not only nodes with `active` status. It can create many pairwise edges if a topic or subject is shared by many nodes, so schedule it with database size/load in mind.

Before running, take or confirm a current backup. Then call the endpoint with steward credentials:

```sh
curl -fsS -X POST http://127.0.0.1:3200/v1/memory/rebuild-edges \
  -H "Authorization: Bearer $MEM_STEWARD_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{}'
```

Expected successful response shape:

```json
{"edges_created":123,"status":"ok"}
```

The endpoint returns the number of generated edges inserted. Verify afterward through `GET /v1/memory/stats`, `GET /v1/memory/edges`, and the `/graph` UI. Do not interpret `edges_created` as the total number of edges: non-generated relationships are retained.

## Troubleshooting checklist

- `memory.service` fails: check `systemctl status memory.service` and `journalctl -u memory.service`; do not print environment-file contents while diagnosing.
- `/health` returns 503: confirm PostgreSQL reachability and the configured database, then inspect service logs.
- `memory.service` exits at startup with "no authentication configured": set `MEM_API_TOKEN` (and `MEM_STEWARD_TOKEN`/`MEM_UI_PASSWORD`) in the environment file.
- API returns 401: verify the bearer token or configured UI Basic credentials without echoing secrets.
- API returns 403 "steward credentials required": the route needs `MEM_STEWARD_TOKEN` or the UI Basic login, not the agent token.
- API returns 403 "cross-origin write rejected": a browser sent a write from another origin; add the proxy's public origin to `MEM_ALLOWED_ORIGINS`.
- Browser UI loops/fails API requests: confirm Basic Auth is enabled/configured consistently and the page is accessed over a trusted network.
- LAN access fails: verify the socket listens only on the intended interface, its address is current, firewall rules allow only trusted clients, and `memory.service` is healthy.
- Graph appears stale: confirm the rebuild request returned success, then refresh the page and check edge statistics.
