# API reference

Base URL used in the examples: `http://127.0.0.1:3200`.

## Authentication

- `/health` is unauthenticated.
- API routes accept `Authorization: Bearer $MEM_API_TOKEN` (agent role) or `Authorization: Bearer $MEM_STEWARD_TOKEN` (steward role) when those tokens are configured.
- If `MEM_UI_PASSWORD` is set, API routes also accept the configured UI Basic Auth credentials, which carry the steward role; this supports the browser UI.
- Every page other than `/health` (the web UI) uses HTTP Basic Auth when `MEM_UI_PASSWORD` is set.
- Browser writes carrying an `Origin` header must come from the service's own host or from `MEM_ALLOWED_ORIGINS`; otherwise they get `403`. Clients that send no `Origin` header (curl, agents) are unaffected.
- Steward-only routes (marked below) return `403` for the agent role.
- Keep tokens/passwords out of shell history, logs, screenshots, and source control. Use HTTPS across untrusted networks.

Shell snippets below assume `MEM_API_TOKEN` is already provided through a secure environment mechanism.

## Health and statistics

```http
GET /health
GET /v1/memory/stats
```

`/health` returns service/database availability. Stats returns aggregate counts and database information.

## Nodes

```http
GET    /v1/memory/nodes?limit=100&status=active
POST   /v1/memory/nodes
GET    /v1/memory/nodes/{id}
DELETE /v1/memory/nodes/{id}
```

Node-list query parameters: `limit` (maximum 5000), `layer`, `topic`, `subject`, and `status`. Omitted status and `status=active` select active nodes; `status=proposed` or `status=retired` filters to that state; `status=all` includes every status.

Example create request:

```sh
curl -sS -X POST http://127.0.0.1:3200/v1/memory/nodes \
  -H "Authorization: Bearer $MEM_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'X-Mem-Source: agent' \
  -d '{"body":"Example durable fact","layer":"semantic","topic":"example","subject":"service"}'
```

`body` is required and must be a non-empty string; `metadata` is stored as a JSON object (a non-object value is wrapped as `{"value": ...}`); `limit` below 1 falls back to 100.

The server assigns `source` and `status` according to writer identity. `X-Mem-Source` takes precedence over JSON `source`; unidentified writes default to `agent`. When `MEM_STEWARD_TOKEN` is configured, only steward credentials produce `active` nodes; an agent claiming a steward label is stored as `unverified:<label>` with status `proposed`. Without `MEM_STEWARD_TOKEN` (legacy mode) identities in `MEM_STEWARD_SOURCE` become `active` on their own say-so, so any API client can claim one.

Single-node deletion is available through `DELETE /v1/memory/nodes/{id}`. System-owned facts (`source=system`) are managed by the refresher and cannot be deleted through this route.

## Edges and graph

```http
GET  /v1/memory/edges
POST /v1/memory/edges
POST /v1/memory/rebuild-edges   (steward only)
```

Create edge JSON can be one edge or an object containing an `edges` array:

```json
{"src":"node-id-1","dst":"node-id-2","rel":"related","weight":1.0}
```

Rebuild example:

```sh
curl -fsS -X POST http://127.0.0.1:3200/v1/memory/rebuild-edges \
  -H "Authorization: Bearer $MEM_STEWARD_TOKEN" \
  -H 'Content-Type: application/json' -d '{}'
```

The operation removes and recreates only generated `same_topic` and `same_subject` edges. It leaves other edge types intact. It is a write operation; take a database backup first when the graph data is important. See [operations](OPERATIONS.md#rebuild-generated-graph-edges).

## Search and context

```http
GET  /v1/memory/search?q=term
POST /v1/memory/search
GET  /v1/memory/context?q=term
POST /v1/memory/context
```

Search returns up to 10 matching active nodes. Context accepts query parameters or JSON, depending on the route's request handler.

## Extraction, brief, and inbox

```http
POST /v1/memory/extract
GET  /v1/memory/brief
GET  /v1/memory/inbox
POST /v1/memory/nodes/{id}/promote   (steward only)
POST /v1/memory/nodes/{id}/reject    (steward only)
```

`/v1/memory/extract` accepts JSON containing `text` and optional `layer`, `topic`, `subject`, and `source`. It creates one node and applies the same writer identity gate as other writes. `/brief` returns active system facts. `/inbox` lists proposed nodes (up to 500). Promote marks an eligible proposed node active; reject retires it. System-owned facts cannot be promoted/rejected.

## Backup and restore

```http
GET  /v1/memory/backup
POST /v1/memory/restore   (steward only)
```

`GET /v1/memory/backup?gzip=true` returns a gzip-compressed backup. The export is one consistent snapshot of every node (including `source`, `status` and `fact_key`) and edge, and it fails with `503` rather than returning a truncated file.

Restore replaces all data in a single transaction: it either restores the whole backup or changes nothing. It requires steward credentials and the header `X-Confirm-Restore: replace-all`. It rejects an empty backup, duplicate node ids, unknown statuses and edges that point at missing nodes, and it limits the decompressed payload size. Send gzip data with `Content-Encoding: gzip` or `Content-Type: application/gzip`. Validate backups against a separate database first.

## Compatibility routes

The service also provides `/api/nodes`, `/api/edges`, `/api/brief`, and `/api/inbox` for compatibility with existing clients. `/` and `/graph` serve the web UI.

## Bulk deletion

Deleting all nodes or all edges is disabled unless the service is started with `MEM_ENABLE_BULK_DELETE=true` and the request includes `X-Confirm-Delete: all`. Avoid enabling this during normal operation.
