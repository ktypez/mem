#!/usr/bin/env python3
"""Minimal stdio MCP server that fronts the local mem knowledge-graph service.

Copy this file, install it (e.g. ~/.local/bin/mem-mcp.py, chmod +x), and register it
with the harness using its local stdio MCP configuration. For OpenCode, consult the
installed version's MCP docs/CLI; configuration syntax can change.

No third-party deps: the MCP JSON-RPC over stdin/stdout is hand-rolled. Writes land as
`proposed` (X-Mem-Source below) unless this identity is explicitly trusted by the service.
Configure the service URL, source identity, and token through environment variables or an
approved secret manager; do not hard-code credentials.
"""
import json
import os
import sys
import urllib.error
import urllib.request

BASE = os.environ.get("MEM_SERVICE_URL", "http://127.0.0.1:3200")
SOURCE = os.environ.get("MEM_SOURCE", "agent")   # set to the calling harness identity


def service_token() -> str:
    """Bearer token for mem. Resolve only from explicit process environment."""
    for key in ("MEM_API_TOKEN", "HERMES_MEMORY_SERVICE_TOKEN"):
        value = os.environ.get(key, "").strip()
        if value:
            return value
    raise RuntimeError("Set MEM_API_TOKEN in the MCP process environment or secret manager")


def call(method: str, path: str, body: dict | None = None) -> dict:
    headers = {"X-Mem-Source": SOURCE}
    token = service_token()
    if token:
        headers["Authorization"] = f"Bearer {token}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read().decode()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        return {"error": f"HTTP {e.code}: {e.read().decode()[:300]}"}
    except Exception as e:  # noqa: BLE001
        return {"error": str(e)}


TOOLS = [
    {
        "name": "memory_search",
        "description": "Search persistent memory for relevant facts and knowledge.",
        "inputSchema": {"type": "object",
            "properties": {"query": {"type": "string"}, "limit": {"type": "integer", "default": 10}},
            "required": ["query"]},
    },
    {
        "name": "memory_add",
        "description": "Store a new fact. Lands in the inbox until a steward promotes it.",
        "inputSchema": {"type": "object",
            "properties": {
                "body": {"type": "string"},
                "layer": {"type": "string", "enum": ["episodic", "semantic", "procedural", "profile"], "default": "semantic"},
                "topic": {"type": "string"}, "subject": {"type": "string"},
                "confidence": {"type": "number", "default": 1.0}},
            "required": ["body"]},
    },
    {
        "name": "memory_context",
        "description": "Get relevant memory context for a query.",
        "inputSchema": {"type": "object",
            "properties": {"query": {"type": "string"}, "limit": {"type": "integer", "default": 5}},
            "required": ["query"]},
    },
    {
        "name": "memory_extract",
        "description": "Extract and store facts from a block of text.",
        "inputSchema": {"type": "object",
            "properties": {"text": {"type": "string"}}, "required": ["text"]},
    },
    {
        "name": "memory_stats",
        "description": "Counts of active / proposed / retired memory nodes.",
        "inputSchema": {"type": "object", "properties": {}},
    },
    {
        "name": "memory_inbox",
        "description": "List proposed nodes awaiting steward review.",
        "inputSchema": {"type": "object",
            "properties": {"limit": {"type": "integer", "default": 50}}},
    },
    {
        "name": "memory_promote",
        "description": "Promote a proposed node to active (trusted).",
        "inputSchema": {"type": "object",
            "properties": {"id": {"type": "string"}}, "required": ["id"]},
    },
    {
        "name": "memory_reject",
        "description": "Reject a proposed node (retires it).",
        "inputSchema": {"type": "object",
            "properties": {"id": {"type": "string"}}, "required": ["id"]},
    },
]


def run_tool(name: str, args: dict) -> dict:
    if name == "memory_search":
        return call("POST", "/v1/memory/search", {"query": args["query"], "limit": args.get("limit", 10)})
    if name == "memory_add":
        return call("POST", "/v1/memory/nodes", {
            "body": args["body"], "layer": args.get("layer", "semantic"),
            "topic": args.get("topic"), "subject": args.get("subject"),
            "confidence": args.get("confidence", 1.0), "bot_id": SOURCE})
    if name == "memory_context":
        return call("POST", "/v1/memory/context", {"query": args["query"], "limit": args.get("limit", 5)})
    if name == "memory_extract":
        return call("POST", "/v1/memory/extract", {"text": args["text"], "bot_id": SOURCE})
    if name == "memory_stats":
        return call("GET", "/v1/memory/stats")
    if name == "memory_inbox":
        return call("GET", "/v1/memory/inbox?limit=%d" % int(args.get("limit", 50)))
    if name == "memory_promote":
        return call("POST", "/v1/memory/nodes/%s/promote" % args["id"])
    if name == "memory_reject":
        return call("POST", "/v1/memory/nodes/%s/reject" % args["id"])
    return {"error": "unknown tool: %s" % name}


def main() -> int:
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            continue
        method = req.get("method")
        rid = req.get("id")
        params = req.get("params") or {}

        if method == "initialize":
            resp = {"jsonrpc": "2.0", "id": rid, "result": {
                "protocolVersion": "2024-11-05", "capabilities": {"tools": {}},
                "serverInfo": {"name": "mem-mcp", "version": "1.0.0"}}}
        elif method.startswith("notifications/"):
            continue
        elif method == "ping":
            resp = {"jsonrpc": "2.0", "id": rid, "result": {}}
        elif method == "tools/list":
            resp = {"jsonrpc": "2.0", "id": rid, "result": {"tools": TOOLS}}
        elif method == "tools/call":
            out = run_tool(params.get("name"), params.get("arguments") or {})
            content = [{"type": "text", "text": json.dumps(out, ensure_ascii=False)}]
            resp = {"jsonrpc": "2.0", "id": rid,
                    "result": {"content": content, "isError": isinstance(out, dict) and "error" in out}}
        else:
            resp = {"jsonrpc": "2.0", "id": rid,
                    "error": {"code": -32601, "message": "method not found: %s" % method}}

        sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
        sys.stdout.flush()
    return 0


if __name__ == "__main__":
    sys.exit(main())
