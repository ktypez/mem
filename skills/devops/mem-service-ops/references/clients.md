# Connecting a different agent harness

Hermes can use mem through its memory plugin. Other harnesses can use the REST API directly or run a stdio MCP bridge. The starter bridge is `../templates/mem-mcp.py`.

## MCP bridge setup

1. Copy the bridge to a stable executable path on the target machine.
2. Set `MEM_SERVICE_URL` (default `http://127.0.0.1:3200`) and provide `MEM_API_TOKEN` through the harness environment or an approved secret manager. Do not put credentials in command-line arguments or source control.
3. Set `MEM_SOURCE` to the harness identity (default `opencode`). Requests include this value as `X-Mem-Source`.
4. Register the bridge in the harness MCP configuration using its local process/stdio transport. For OpenCode, use its current MCP configuration format and verify with `opencode mcp list`; configuration/CLI syntax may vary by installed version.
5. Verify MCP initialize and tools/list before making writes. Try a read-only stats call, then (if needed) create a uniquely marked probe, verify it, delete it, and confirm cleanup.

The service token resolution in the starter script checks process environment first, then the current user's Hermes `.env`, then `/etc/mem/mem.env`. Prefer environment injection or a secret manager; avoid broadening permissions on root-owned files merely to make a bridge work. Never print the token or DSN.

## Writer trust and visibility

- `X-Mem-Source` is the writer identity and takes precedence over JSON `source`.
- Identities in `MEM_STEWARD_SOURCE` are trusted and create active nodes. Other identities create proposed nodes for steward review.
- Do not add a harness to the trusted list unless the user explicitly wants its writes active immediately. A shared API token does not distinguish callers, so allowing a client to assert a trusted source is a trust boundary.
- Default search/context reads active nodes. Proposed writes are visible through the inbox endpoint.

## MCP protocol smoke test

From the directory containing the bridge, send initialize and tools/list requests. Avoid live mutation calls unless you record and clean up the probe:

```sh
printf '%s\n' \
'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
'{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
| python3 templates/mem-mcp.py
```

The expected output is two JSON-RPC responses; notifications do not receive a response. Use the bridge's `memory_stats` tool for a read-only service check.
