# Security and deployment notes

## Required environment
The service no longer has a built-in PostgreSQL username/password. Earlier commits in this repository's public git history contain a default database credential. Rewriting history does not undo that exposure, so **rotate the PostgreSQL password** on any existing deployment, and confirm PostgreSQL only listens on localhost or a trusted network. Configure `/etc/mem/mem.env` as root with mode `0600`:

```sh
sudo install -d -m 0750 /etc/mem
sudo install -m 0600 /dev/null /etc/mem/mem.env
sudoedit /etc/mem/mem.env
```

Set values appropriate for your host (replace examples; do not commit this file):

```ini
DATABASE_URL=postgres://mem:REPLACE_WITH_STRONG_PASSWORD@127.0.0.1:5432/mem?sslmode=disable
MEM_LISTEN_ADDR=127.0.0.1:3200
MEM_API_TOKEN=REPLACE_WITH_A_LONG_RANDOM_SECRET
MEM_STEWARD_TOKEN=REPLACE_WITH_ANOTHER_LONG_RANDOM_SECRET
MEM_UI_USER=admin
MEM_UI_PASSWORD=REPLACE_WITH_A_THIRD_LONG_RANDOM_SECRET
# Leave bulk deletion disabled unless temporarily required.
# MEM_ENABLE_BULK_DELETE=true
```

Generate each secret with `openssl rand -hex 32`. Treat them as passwords. Send tokens to API clients as `Authorization: Bearer <token>`. The web UI does not prompt for a token: it relies on HTTP Basic Auth (`MEM_UI_USER`/`MEM_UI_PASSWORD`), so set `MEM_UI_PASSWORD` if you use the dashboard.

There are two roles. `MEM_API_TOKEN` is the agent role: it reads and writes, but writes are only ever `proposed`. `MEM_STEWARD_TOKEN` and the UI Basic login are the steward role: they write `active` nodes, review the inbox, rebuild edges and restore backups. Give agents only the agent token. If `MEM_STEWARD_TOKEN` is unset the service runs in legacy mode where the `X-Mem-Source` header is trusted, so any API client can claim to be the steward; it logs a warning and this is not a security boundary.

The service defaults to loopback-only (`127.0.0.1:3200`) and refuses to start with no credential configured unless `MEM_ALLOW_UNAUTHENTICATED=true` is set, which is for local development only. A socket proxy or reverse proxy in front of a loopback listener makes every remote client look local, so never combine that opt-out with a proxy. Plain HTTP exposes tokens and passwords on the network: use TLS or an encrypted tunnel for anything beyond the local host. Do not expose PostgreSQL or the service directly to the public internet.

## Browser protections
The web UI is served with a per-response Content-Security-Policy nonce (no inline handlers), `X-Frame-Options: DENY`, and `Referrer-Policy: no-referrer`. API writes that carry a cross-site `Origin` header are rejected (add legitimate proxy origins to `MEM_ALLOWED_ORIGINS`), and restore additionally requires the `X-Confirm-Restore: replace-all` header, which browsers cannot send cross-site without a CORS preflight.

## Destructive operations
Bulk deletion of all nodes or all edges is disabled unless both conditions are met:
- `MEM_ENABLE_BULK_DELETE=true`
- Request header `X-Confirm-Delete: all`

Avoid enabling this for normal operation. Single-node deletion still requires API authentication when a credential is configured. HTTP restore is separately protected: it needs steward credentials and `X-Confirm-Restore: replace-all`, and it runs in one transaction so a failed restore changes nothing.

## Backup and restore
Make backups outside the application and store them somewhere separate from the database host. Example for a local PostgreSQL instance:

```sh
umask 077
pg_dump --format=custom --file="/var/backups/mem-$(date +%F-%H%M%S).dump" mem
```

Restore into a separate test database before relying on a backup:

```sh
createdb mem_restore_test
pg_restore --dbname=mem_restore_test /path/to/backup.dump
```

Automate backups and retention with your host scheduler, monitor failures, and periodically test restore. Use the correct PostgreSQL role and database for your installation.

## Runtime changes
After configuring the environment file:

```sh
sudo systemctl daemon-reload
sudo systemctl restart memory.service
sudo systemctl status memory.service
curl http://127.0.0.1:3200/health
```

If your unit has a different name, substitute it. The health endpoint reports database query failures as HTTP 503.

## Remaining work
- Add integration tests against a disposable PostgreSQL database.
- Add rate limiting and request-level audit logging at the reverse proxy.
- Consider per-agent tokens (one identity per token) if several agents have different trust levels; today all agents share the agent role.
- Run the service as a dedicated unprivileged user with `ProtectHome=true`, and move the live-system fact collector out of the API process (it executes host commands such as `crontab -l`, whose lines may contain secrets).
- Replace the CDN-loaded `d3` and web fonts with vendored copies so the CSP can be tightened.
- Test restore and graph rebuild against production-sized copies before deployment.
