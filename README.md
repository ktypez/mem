# mem

`mem` is a self-hosted memory service and knowledge graph. It stores memory nodes and relationships in PostgreSQL, exposes a JSON HTTP API, and serves a browser UI at `/` and `/graph`.

## Features

- PostgreSQL-backed memory nodes, edges, search, and session context.
- Web UI for browsing the graph and memory records.
- Writer identity and review inbox: trusted writers can create active nodes; other writes are marked `proposed` for review.
- Live system-fact refresh and steward inbox endpoints.
- JSON backup and restore, including optional gzip backups.
- Graph edge rebuild for `same_topic` and `same_subject` relationships.

## Requirements

- Go version declared in [`go.mod`](go.mod) (currently Go 1.27.3).
- PostgreSQL.
- A dedicated database and least-privilege PostgreSQL role.

## Build and run

```sh
go test ./...
go build -o mem .
```

Configure the process environment before starting it:

```ini
DATABASE_URL=postgres://mem:REPLACE_ME@127.0.0.1:5432/mem?sslmode=disable
MEM_LISTEN_ADDR=127.0.0.1:3200
MEM_API_TOKEN=REPLACE_WITH_A_LONG_RANDOM_SECRET
MEM_UI_USER=admin
MEM_UI_PASSWORD=REPLACE_WITH_A_DIFFERENT_LONG_SECRET
# Optional comma-separated identities allowed to write directly to active:
MEM_STEWARD_SOURCE=steward,hermes
```

Keep secrets in a root-owned environment file or secret manager; do not commit them. The service initializes/updates its tables on startup. For local development, load those variables from a protected file and run:

```sh
./mem server
```

The server must not be bound to a public interface without a valid `MEM_API_TOKEN`. Prefer binding to `127.0.0.1` and exposing it only through an authenticated, TLS-terminating reverse proxy. See [SECURITY.md](SECURITY.md) and [deployment and operations](docs/OPERATIONS.md).

## First check

```sh
curl -fsS http://127.0.0.1:3200/health
curl -fsS -H "Authorization: Bearer $MEM_API_TOKEN" \
  http://127.0.0.1:3200/v1/memory/stats
```

Open `http://127.0.0.1:3200/graph` in a browser. If `MEM_UI_PASSWORD` is set, the UI uses HTTP Basic Authentication. API clients use the bearer token; the UI can also use its Basic credentials for API requests.

## Memory review model

Every write has a source identity. `X-Mem-Source` takes precedence; otherwise the JSON `source` field is used, with `agent` as the default. Identities listed in `MEM_STEWARD_SOURCE` (default: `steward`) write `active` nodes. Other writers create `proposed` nodes, visible in the inbox, until reviewed. Normal node listing and search default to active records; request `status=proposed`, `status=retired`, or `status=all` to change the node-list filter. See [API reference](docs/API.md).

## CLI

```sh
./mem help
./mem backup --file backup.json
./mem backup --file backup.json.gz --gzip
./mem restore --file backup.json
```

Backup and restore use `DATABASE_URL` and connect directly to PostgreSQL. Store backups securely and test restores against a separate database before relying on them.

## Agent skill

The repository includes a portable `mem-service-ops` skill for multiple AI agent runtimes. Use `./scripts/install-agent-skill.sh` for standard user-level locations or the runtime's repository-local skill path. See [`skills/README.md`](skills/README.md) for supported clients and install options.

## Documentation

- [API reference](docs/API.md)
- [Deployment, backups, and graph operations](docs/OPERATIONS.md)
- [Security notes](SECURITY.md)
- [Agent skills and installation](skills/README.md)
