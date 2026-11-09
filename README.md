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
# Separate credential for the steward role (see "Roles" below). Must differ from MEM_API_TOKEN.
MEM_STEWARD_TOKEN=REPLACE_WITH_ANOTHER_LONG_RANDOM_SECRET
MEM_UI_USER=admin
MEM_UI_PASSWORD=REPLACE_WITH_A_DIFFERENT_LONG_SECRET
# Optional comma-separated identities used to label steward writes:
MEM_STEWARD_SOURCE=steward,hermes
# Optional comma-separated browser origins allowed to write when a reverse
# proxy rewrites the Host header, e.g. https://mem.example.com
# MEM_ALLOWED_ORIGINS=
# Optional semantic search. Leave both unset to run keyword-only search; set an
# OpenAI-compatible /embeddings endpoint and model to enable it. Requires the
# pgvector extension in the database.
# MEM_EMBED_URL=https://api.example.com/v1
# MEM_EMBED_MODEL=bge-m3
# MEM_EMBED_API_KEY=REPLACE_WITH_PROVIDER_TOKEN
# MEM_EMBED_DIM=1024
# MEM_EMBED_INTERVAL_SECONDS=120
# MEM_EMBED_BATCH=16
```

Keep secrets in a root-owned environment file or secret manager; do not commit them. The service initializes/updates its tables on startup. For local development, load those variables from a protected file and run:

```sh
./mem server
```

The server refuses to start when no credential is configured, unless `MEM_ALLOW_UNAUTHENTICATED=true` is set (local development only). It also refuses a non-loopback bind without `MEM_API_TOKEN` or `MEM_STEWARD_TOKEN`. Prefer binding to `127.0.0.1` and exposing it only through an authenticated, TLS-terminating reverse proxy. See [SECURITY.md](SECURITY.md) and [deployment and operations](docs/OPERATIONS.md).

## Roles

- **agent**: holds `MEM_API_TOKEN`. Can read and write, but every write lands in the inbox as `proposed`.
- **steward**: holds `MEM_STEWARD_TOKEN`, or signs in to the web UI with `MEM_UI_USER`/`MEM_UI_PASSWORD`. Can write `active` nodes, promote or reject inbox items, rebuild edges, and restore backups.

If `MEM_STEWARD_TOKEN` is not set the service runs in legacy mode: the `X-Mem-Source` header is trusted as-is, so any API client can claim to be the steward. It logs a warning at startup. Set `MEM_STEWARD_TOKEN` to make the steward role a real boundary.

## First check

```sh
curl -fsS http://127.0.0.1:3200/health
curl -fsS -H "Authorization: Bearer $MEM_API_TOKEN" \
  http://127.0.0.1:3200/v1/memory/stats
```

Open `http://127.0.0.1:3200/graph` in a browser. If `MEM_UI_PASSWORD` is set, the UI uses HTTP Basic Authentication. API clients use the bearer token; the UI can also use its Basic credentials for API requests.

## Semantic search (optional)

Keyword search (`ILIKE`) is the default and needs no model. Setting `MEM_EMBED_URL` and `MEM_EMBED_MODEL` enables an optional semantic layer:

- The service adds a `vector` column (`MEM_EMBED_DIM`, default 1024), creates an HNSW cosine index, and fills embeddings in a background indexer — never in the request path.
- Any OpenAI-compatible `/embeddings` endpoint works. `MEM_EMBED_API_KEY` is read from the environment file, never from a client request.
- Search still returns keyword hits first and tops up the remaining slots with semantically similar nodes, so a slow or unavailable provider degrades to keyword-only instead of failing.
- Re-embedding is per node: new writes are picked up automatically; after changing the model or dimension, existing vectors must be regenerated.

Requirements: the `vector` extension must be installed in the database. `CREATE EXTENSION` needs a superuser once, then the service's own role can use it.

## Memory review model

Every write has a source identity. `X-Mem-Source` takes precedence; otherwise the JSON `source` field is used, with `agent` as the default. With `MEM_STEWARD_TOKEN` set, only steward credentials write `active` nodes; an agent that claims a steward label is stored as `unverified:<label>` and stays `proposed`. In legacy mode, identities listed in `MEM_STEWARD_SOURCE` (default: `steward`) write `active` nodes. Other writers create `proposed` nodes, visible in the inbox, until reviewed. Normal node listing and search default to active records; request `status=proposed`, `status=retired`, or `status=all` to change the node-list filter. See [API reference](docs/API.md).

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
