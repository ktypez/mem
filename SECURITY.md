# Security and deployment notes

## Required environment
The service no longer has a built-in PostgreSQL username/password. Configure `/etc/mem/mem.env` as root with mode `0600`:

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
# Leave bulk deletion disabled unless temporarily required.
# MEM_ENABLE_BULK_DELETE=true
```

Generate a token with `openssl rand -hex 32`. Treat it as a password. Send it to API clients as `Authorization: Bearer <token>`. The redesigned dashboard asks for the token on its first API request and stores it in session storage for the current tab only.

The service defaults to loopback-only (`127.0.0.1:3200`). It refuses a non-loopback bind unless `MEM_API_TOKEN` is configured. Keep a reverse proxy in front of it and use HTTPS for remote access. Do not expose PostgreSQL or the service directly to the public internet.

## Destructive operations
Bulk deletion of all nodes or all edges is disabled unless both conditions are met:
- `MEM_ENABLE_BULK_DELETE=true`
- Request header `X-Confirm-Delete: all`

Avoid enabling this for normal operation. Single-node deletion still requires API authentication when a token is configured.

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
- Consider separate read/write tokens or per-agent authorization if multiple agents have different trust levels.
- Test restore and graph rebuild against production-sized copies before deployment.
