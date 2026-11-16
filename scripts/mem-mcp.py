#!/usr/bin/env python3
"""Minimal stdio MCP server that fronts the local mem wiki service.

Reads the service token from ~/.hermes/.env or /etc/mem/mem.env (admin-readable)
so other agent CLIs (opencode, pi-bolt, ...) can call the same memory store
Hermes uses. No third-party deps: the MCP JSON-RPC over stdin/stdout is
hand-rolled.

v2 (wiki mode): pages are markdown files; a write appends a fact to its page and
is active immediately — there is no review inbox, so the legacy
inbox/promote/reject tools are gone. `memory_brief` is the cheap session-start
index; `memory_page` reads one page in full.

Install: copy to ~/.local/bin/mem-mcp.py (mode 0755). Keep this file as the
source of truth in the mem repository (scripts/mem-mcp.py).
"""
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

BASE = os.environ.get("MEM_SERVICE_URL", "http://127.0.0.1:3200")


def detect_source() -> str:
    """Who is calling: MEM_SOURCE, else the nearest agent CLI in the process tree.

    Provenance only — the service records the writer, it does not gate on it.
    Both opencode and pi-bolt spawn this bridge, so the caller is found by
    walking up the parent chain instead of hardcoding one runtime.
    """
    env = os.environ.get("MEM_SOURCE", "").strip()
    if env:
        return env
    hints = ("pi-bolt", "opencode", "codex", "claude", "gemini", "hermes", "pi")
    pid = os.getpid()
    for _ in range(3):
        try:
            with open("/proc/%d/stat" % pid, "r", encoding="utf-8") as fh:
                ppid = int(fh.read().rsplit(")", 1)[1].split()[1])
            with open("/proc/%d/comm" % ppid, "r", encoding="utf-8") as fh:
                comm = fh.read().strip()
            with open("/proc/%d/cmdline" % ppid, "rb") as fh:
                cmdline = fh.read().decode("utf-8", "replace")
        except (OSError, IndexError, ValueError):
            break
        blob = (comm + " " + cmdline).lower()
        for hint in hints:
            if comm == hint or comm.startswith(hint) or ("/" + hint) in blob or (" " + hint + " ") in blob:
                return "pi" if hint.startswith("pi") else hint
        pid = ppid
    return "agent"


SOURCE = detect_source()


def service_token() -> str:
    """Bearer token for the mem service.

    The service requires Authorization on every route except /health. The token
    lives in ~/.hermes/.env (admin-readable) and, for root-owned installs,
    /etc/mem/mem.env; the process environment is not trusted.
    """
    for key in ("MEM_API_TOKEN", "HERMES_MEMORY_SERVICE_TOKEN"):
        value = os.environ.get(key, "").strip()
        if value:
            return value
    for path in (os.path.expanduser("~/.hermes/.env"), "/etc/mem/mem.env"):
        try:
            with open(path, "r", encoding="utf-8") as fh:
                for line in fh:
                    line = line.strip()
                    if not line or line.startswith("#") or "=" not in line:
                        continue
                    k, _, v = line.partition("=")
                    if k.strip() in ("MEM_API_TOKEN", "HERMES_MEMORY_SERVICE_TOKEN"):
                        v = v.strip().strip('"').strip("'")
                        if v:
                            return v
        except OSError:
            continue
    return ""


def call(method: str, path: str, body: dict | None = None, raw_text: bool = False):
    """Call the service; returns parsed JSON, or the raw body when raw_text."""
    url = BASE + path
    headers = {"X-Mem-Source": SOURCE, "Accept": "text/plain, application/json"}
    token = service_token()
    if token:
        headers["Authorization"] = f"Bearer {token}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read().decode()
            if raw_text:
                return raw
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        return f"HTTP {e.code}: {e.read().decode()[:300]}" if raw_text else {"error": f"HTTP {e.code}: {e.read().decode()[:300]}"}
    except Exception as e:  # noqa: BLE001 - surface any failure to the caller
        return str(e) if raw_text else {"error": str(e)}


TOOLS = [
    {
        "name": "memory_brief",
        "description": (
            "Cheap index of what this memory store already knows: one line per page "
            "(slug, tags, updated date, first fact). Call it once at the start of a "
            "session, then open only the pages a task needs with memory_page. "
            "Do not call it after every turn."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {"limit": {"type": "integer", "default": 10}},
        },
    },
    {
        "name": "memory_search",
        "description": (
            "Full-text search across all memory pages (substring/trigram, works for "
            "Thai). Returns one compact line per hit (slug, tags, matching text) — "
            "open a hit with memory_page. Use it before answering questions about "
            "this host, the user or their projects, after memory_brief or instead "
            "of it when the query is specific."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "query": {"type": "string"},
                "limit": {"type": "integer", "default": 10},
            },
            "required": ["query"],
        },
    },
    {
        "name": "memory_page",
        "description": "Read one memory page in full by its slug (from memory_brief or memory_search).",
        "inputSchema": {
            "type": "object",
            "properties": {"slug": {"type": "string"}},
            "required": ["slug"],
        },
    },
    {
        "name": "memory_add",
        "description": (
            "Append one durable fact to the page for its topic (page = slug, created "
            "on first use). Active immediately; recorded with the caller as provenance. "
            "Use for facts worth remembering in future sessions, not for narration. "
            "Never store secrets or tokens."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "body": {"type": "string", "description": "The fact, one line."},
                "page": {"type": "string", "description": "Slug of the page to append to (default: Inbox)."},
            },
            "required": ["body"],
        },
    },
    {
        "name": "memory_extract",
        "description": "Extract durable facts from a block of text (a turn, a log, notes) and file them.",
        "inputSchema": {
            "type": "object",
            "properties": {"text": {"type": "string"}},
            "required": ["text"],
        },
    },
    {
        "name": "memory_context",
        "description": "Relevant memory context for a query, ready to paste into the conversation.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "query": {"type": "string"},
                "limit": {"type": "integer", "default": 5},
            },
            "required": ["query"],
        },
    },
    {
        "name": "memory_stats",
        "description": "Page counts and service health.",
        "inputSchema": {"type": "object", "properties": {}},
    },
]


def run_tool(name: str, args: dict):
    if name == "memory_brief":
        return call("GET", "/v1/memory/brief?format=text&limit=%d" % int(args.get("limit", 10)), raw_text=True)
    if name == "memory_search":
        return call("GET", "/v1/memory/search?q=%s&limit=%d&format=text"
                    % (urllib.parse.quote(args["query"], safe=""), int(args.get("limit", 10))), raw_text=True)
    if name == "memory_page":
        slug = urllib.parse.quote(args["slug"], safe="")
        return call("GET", "/v1/wiki/pages/%s?format=text" % slug, raw_text=True)
    if name == "memory_add":
        return call("POST", "/v1/memory/nodes", {
            "body": args["body"],
            # topic is the page slug the fact is appended to (v2 wiki store).
            "topic": args.get("page") or args.get("topic") or "Inbox",
            "source": SOURCE,
        })
    if name == "memory_extract":
        return call("POST", "/v1/memory/extract", {"text": args["text"], "source": SOURCE})
    if name == "memory_context":
        return call("POST", "/v1/memory/context", {"query": args["query"], "limit": args.get("limit", 5)})
    if name == "memory_stats":
        return call("GET", "/v1/memory/stats?format=text", raw_text=True)
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
            resp = {
                "jsonrpc": "2.0",
                "id": rid,
                "result": {
                    "protocolVersion": "2024-11-05",
                    "capabilities": {"tools": {}},
                    "serverInfo": {"name": "mem-mcp", "version": "2.0.0"},
                },
            }
        elif method == "notifications/initialized" or method.startswith("notifications/"):
            continue
        elif method == "ping":
            resp = {"jsonrpc": "2.0", "id": rid, "result": {}}
        elif method == "tools/list":
            resp = {"jsonrpc": "2.0", "id": rid, "result": {"tools": TOOLS}}
        elif method == "tools/call":
            name = params.get("name") or ""
            args = params.get("arguments") or {}
            out = run_tool(name, args)
            if isinstance(out, str):
                content = [{"type": "text", "text": out}]
                is_error = out.startswith("HTTP ")
            else:
                content = [{"type": "text", "text": json.dumps(out, ensure_ascii=False)}]
                is_error = isinstance(out, dict) and "error" in out
            resp = {"jsonrpc": "2.0", "id": rid, "result": {"content": content, "isError": is_error}}
        else:
            resp = {"jsonrpc": "2.0", "id": rid, "error": {"code": -32601, "message": "method not found: %s" % method}}

        sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
        sys.stdout.flush()
    return 0


if __name__ == "__main__":
    sys.exit(main())
