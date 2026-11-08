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

   Configure `DATABASE_URL`, `MEM_LISTEN_ADDR=127.0.0.1:3200`, `MEM_API_TOKEN`, and, when the UI is exposed, `MEM_UI_USER` and `MEM_UI_PASSWORD`. Optionally set `MEM_STEWARD_SOURCE=steward,hermes` for trusted writer identities. Keep credentials out of commands, logs, documentation, and repository files.
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

## Network exposure

Keep the Go service bound to loopback. For remote access, use a suitably authenticated HTTPS reverse proxy or an explicitly scoped local-network socket proxy. Never bind to `0.0.0.0` as a shortcut. The sample `mem-lan.socket` and `mem-lan.service` are specific to a LAN interface/IP and must be reviewed for the target host, firewall, and authentication requirements; they are not a generic internet-facing deployment recipe. `/health` does not require authentication, so restrict network exposure even when API and UI authentication are configured.

## Backups and restore drills

The CLI uses `DATABASE_URL` and exports/imports the database as JSON:

```sh
umask 077
./mem backup --file /secure/backups/mem-$(date +%F-%H%M%S).json
./mem backup --file /secure/backups/mem-$(date +%F-%H%M%S).json.gz --gzip
```

The CLI restore operation imports a JSON backup; it does not perform a PostgreSQL schema snapshot or replace the `pg_dump` workflow. For full database recovery, use a protected PostgreSQL backup such as:

```sh
umask 077
pg_dump --format=custom --file="/secure/backups/mem-$(date +%F-%H%M%S).dump" "$PGDATABASE"
```

Keep backups off-host, enforce retention, monitor job failures, and periodically restore into a separate test database. Do not test restores against the production database. The HTTP restore API is also a write operation and should be treated as potentially destructive.

## Rebuild generated graph edges

`POST /v1/memory/rebuild-edges` deletes and recreates only `same_topic` and `same_subject` relationships. Manually created relationships and other relation types remain. Rebuilding considers all nodes, not only nodes with `active` status. It can create many pairwise edges if a topic or subject is shared by many nodes, so schedule it with database size/load in mind.

Before running, take or confirm a current backup. Then call the authenticated endpoint:

```sh
curl -fsS -X POST http://127.0.0.1:3200/v1/memory/rebuild-edges \
  -H "Authorization: Bearer $MEM_API_TOKEN" \
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
- API returns 401: verify the bearer token or configured UI Basic credentials without echoing secrets.
- Browser UI loops/fails API requests: confirm Basic Auth is enabled/configured consistently and the page is accessed over a trusted network.
- LAN access fails: verify the socket listens only on the intended interface, its address is current, firewall rules allow only trusted clients, and `memory.service` is healthy.
- Graph appears stale: confirm the rebuild request returned success, then refresh the page and check edge statistics.
