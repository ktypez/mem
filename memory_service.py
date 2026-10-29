#!/usr/bin/env python3
"""
Memory Service — extracted from Memoh's memory architecture, rebuilt for Hermes.

Implements a wiki-style knowledge graph (nodes + edges) over SQLite,
mirroring Memoh's wikistore.Store interface but using local storage.

Features:
  - Memory nodes: facts/knowledge with metadata, confidence, layer, topic
  - Memory edges: relationships between nodes (refs, same_topic, same_day, etc.)
  - Full-text search over node bodies
  - LLM-based fact extraction from text (optional)
  - REST API on port 3200
  - SQLite backend (no PostgreSQL dependency)

NodeSpec (mirrors memoh migrate.NodeSpec):
  id, bot_id, body, hash, layer, fact_type, subject, confidence,
  metadata, source_message_ids, profile_ref, topic, captured_at, expires_at

EdgeSpec (mirrors memoh migrate.EdgeSpec):
  src, dst, kind, weight, metadata

Memory layers (memoh MemoryLayer):
  episodic, semantic, procedural, profile

Fact types:
  preference, fact, event, skill, goal, constraint
"""
import hashlib
import json
import os
import re
import sqlite3
import time
import urllib.request
import urllib.error
from datetime import datetime, timedelta
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from urllib.parse import urlparse, parse_qs

# Configuration
MEMORY_DB = Path("~/.hermes/memory_service.db").expanduser()
MEMORY_PORT = 3200
DEFAULT_BOT_ID = "hermes"

# Schema (mirrors memoh migrate tables)
SCHEMA = """
CREATE TABLE IF NOT EXISTS memory_nodes (
    id TEXT PRIMARY KEY,
    bot_id TEXT NOT NULL,
    body TEXT NOT NULL,
    hash TEXT NOT NULL,
    layer TEXT NOT NULL DEFAULT 'semantic',
    fact_type TEXT NOT NULL DEFAULT 'fact',
    subject TEXT,
    confidence REAL NOT NULL DEFAULT 1.0,
    metadata TEXT DEFAULT '{}',
    source_message_ids TEXT DEFAULT '[]',
    profile_ref TEXT,
    topic TEXT,
    captured_at TEXT NOT NULL,
    expires_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS memory_edges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bot_id TEXT NOT NULL,
    src TEXT NOT NULL,
    dst TEXT NOT NULL,
    kind TEXT NOT NULL,
    weight REAL NOT NULL DEFAULT 1.0,
    metadata TEXT DEFAULT '{}',
    created_at TEXT NOT NULL,
    FOREIGN KEY (src) REFERENCES memory_nodes(id) ON DELETE CASCADE,
    FOREIGN KEY (dst) REFERENCES memory_nodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_nodes_bot ON memory_nodes(bot_id);
CREATE INDEX IF NOT EXISTS idx_nodes_layer ON memory_nodes(bot_id, layer);
CREATE INDEX IF NOT EXISTS idx_nodes_topic ON memory_nodes(bot_id, topic);
CREATE INDEX IF NOT EXISTS idx_nodes_subject ON memory_nodes(bot_id, subject);
CREATE INDEX IF NOT EXISTS idx_edges_bot ON memory_edges(bot_id);
CREATE INDEX IF NOT EXISTS idx_edges_src ON memory_edges(src);
CREATE INDEX IF NOT EXISTS idx_edges_dst ON memory_edges(dst);

-- Full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    body, subject, topic, content=memory_nodes, content_rowid=rowid
);

CREATE TRIGGER IF NOT EXISTS memory_nodes_ai AFTER INSERT ON memory_nodes BEGIN
    INSERT INTO memory_fts(rowid, body, subject, topic)
    VALUES (new.rowid, new.body, new.subject, new.topic);
END;

CREATE TRIGGER IF NOT EXISTS memory_nodes_ad AFTER DELETE ON memory_nodes BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, body, subject, topic)
    VALUES ('delete', old.rowid, old.body, old.subject, old.topic);
END;

CREATE TRIGGER IF NOT EXISTS memory_nodes_au AFTER UPDATE ON memory_nodes BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, body, subject, topic)
    VALUES ('delete', old.rowid, old.body, old.subject, old.topic);
    INSERT INTO memory_fts(rowid, body, subject, topic)
    VALUES (new.rowid, new.body, new.subject, new.topic);
END;
"""


def get_db():
    conn = sqlite3.connect(str(MEMORY_DB))
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA foreign_keys = ON")
    return conn


def init_db():
    MEMORY_DB.parent.mkdir(parents=True, exist_ok=True)
    conn = get_db()
    conn.executescript(SCHEMA)
    conn.commit()
    conn.close()


def now_iso():
    return datetime.utcnow().isoformat() + "Z"


def make_node_id(bot_id, body):
    """Generate a deterministic node ID from bot_id + body hash."""
    h = hashlib.sha256(f"{bot_id}:{body}".encode()).hexdigest()[:16]
    return f"mem_{h}"


def node_to_dict(row):
    if row is None:
        return None
    d = dict(row)
    d["metadata"] = json.loads(d.get("metadata", "{}") or "{}")
    d["source_message_ids"] = json.loads(d.get("source_message_ids", "[]") or "[]")
    return d


class MemoryStore:
    """Backend-agnostic memory store (mirrors memoh wikistore.Store)."""

    def __init__(self, db_path=None):
        self.db_path = db_path or MEMORY_DB
        init_db()

    def _conn(self):
        conn = sqlite3.connect(str(self.db_path))
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA foreign_keys = ON")
        return conn

    # --- Node operations ---

    def upsert_node(self, node):
        conn = self._conn()
        node_id = node.get("id") or make_node_id(node.get("bot_id", DEFAULT_BOT_ID), node["body"])
        bot_id = node.get("bot_id", DEFAULT_BOT_ID)
        body = node["body"]
        h = hashlib.sha256(body.encode()).hexdigest()
        layer = node.get("layer", "semantic")
        fact_type = node.get("fact_type", "fact")
        subject = node.get("subject")
        confidence = node.get("confidence", 1.0)
        metadata = json.dumps(node.get("metadata", {}))
        source_ids = json.dumps(node.get("source_message_ids", []))
        profile_ref = node.get("profile_ref")
        topic = node.get("topic")
        captured_at = node.get("captured_at", now_iso())
        expires_at = node.get("expires_at")
        now = now_iso()

        conn.execute("""
            INSERT INTO memory_nodes (id, bot_id, body, hash, layer, fact_type, subject,
                confidence, metadata, source_message_ids, profile_ref, topic,
                captured_at, expires_at, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET
                body=excluded.body, hash=excluded.hash, layer=excluded.layer,
                fact_type=excluded.fact_type, subject=excluded.subject,
                confidence=excluded.confidence, metadata=excluded.metadata,
                source_message_ids=excluded.source_message_ids,
                profile_ref=excluded.profile_ref, topic=excluded.topic,
                captured_at=excluded.captured_at, expires_at=excluded.expires_at,
                updated_at=excluded.updated_at
        """, (node_id, bot_id, body, h, layer, fact_type, subject, confidence,
              metadata, source_ids, profile_ref, topic, captured_at, expires_at, now, now))
        conn.commit()

        row = conn.execute("SELECT * FROM memory_nodes WHERE id = ?", (node_id,)).fetchone()
        conn.close()
        return node_to_dict(row)

    def get_node(self, bot_id, node_id):
        conn = self._conn()
        row = conn.execute(
            "SELECT * FROM memory_nodes WHERE bot_id = ? AND id = ?", (bot_id, node_id)
        ).fetchone()
        conn.close()
        return node_to_dict(row)

    def list_nodes(self, bot_id, layer=None, topic=None, subject=None, limit=100):
        conn = self._conn()
        query = "SELECT * FROM memory_nodes WHERE bot_id = ?"
        params = [bot_id]
        if layer:
            query += " AND layer = ?"
            params.append(layer)
        if topic:
            query += " AND topic = ?"
            params.append(topic)
        if subject:
            query += " AND subject = ?"
            params.append(subject)
        query += " ORDER BY captured_at ASC LIMIT ?"
        params.append(limit)
        rows = conn.execute(query, params).fetchall()
        conn.close()
        return [node_to_dict(r) for r in rows]

    def delete_node(self, bot_id, node_id):
        conn = self._conn()
        # Delete incident edges first
        conn.execute("DELETE FROM memory_edges WHERE bot_id = ? AND (src = ? OR dst = ?)",
                     (bot_id, node_id, node_id))
        conn.execute("DELETE FROM memory_nodes WHERE bot_id = ? AND id = ?", (bot_id, node_id))
        conn.commit()
        conn.close()

    def delete_all_nodes(self, bot_id):
        conn = self._conn()
        conn.execute("DELETE FROM memory_edges WHERE bot_id = ?", (bot_id,))
        conn.execute("DELETE FROM memory_nodes WHERE bot_id = ?", (bot_id,))
        conn.commit()
        conn.close()

    def count_nodes(self, bot_id):
        conn = self._conn()
        count = conn.execute("SELECT COUNT(*) FROM memory_nodes WHERE bot_id = ?", (bot_id,)).fetchone()[0]
        conn.close()
        return count

    # --- Edge operations ---

    def upsert_edges(self, edges):
        conn = self._conn()
        now = now_iso()
        for e in edges:
            conn.execute("""
                INSERT INTO memory_edges (bot_id, src, dst, kind, weight, metadata, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT DO NOTHING
            """, (e.get("bot_id", DEFAULT_BOT_ID), e["src"], e["dst"], e["kind"],
                  e.get("weight", 1.0), json.dumps(e.get("metadata", {})), now))
        conn.commit()
        conn.close()

    def list_edges(self, bot_id):
        conn = self._conn()
        rows = conn.execute("SELECT * FROM memory_edges WHERE bot_id = ?", (bot_id,)).fetchall()
        conn.close()
        result = []
        for r in rows:
            d = dict(r)
            d["metadata"] = json.loads(d.get("metadata", "{}") or "{}")
            result.append(d)
        return result

    def delete_edges_for_node(self, bot_id, node_id):
        conn = self._conn()
        conn.execute("DELETE FROM memory_edges WHERE bot_id = ? AND (src = ? OR dst = ?)",
                     (bot_id, node_id, node_id))
        conn.commit()
        conn.close()

    def delete_all_edges(self, bot_id):
        conn = self._conn()
        conn.execute("DELETE FROM memory_edges WHERE bot_id = ?", (bot_id,))
        conn.commit()
        conn.close()

    def count_edges(self, bot_id):
        conn = self._conn()
        count = conn.execute("SELECT COUNT(*) FROM memory_edges WHERE bot_id = ?", (bot_id,)).fetchone()[0]
        conn.close()
        return count

    def rebuild_derived_edges(self, bot_id):
        """Recompute derived edges (same_topic, same_day, same_subject, refs)."""
        conn = self._conn()
        # Clear existing derived edges
        conn.execute("DELETE FROM memory_edges WHERE bot_id = ? AND kind IN ('same_topic', 'same_day', 'same_subject', 'same_layer')",
                     (bot_id,))

        nodes = conn.execute("SELECT * FROM memory_nodes WHERE bot_id = ?", (bot_id,)).fetchall()
        nodes = [dict(n) for n in nodes]
        now = now_iso()
        edges_added = 0

        # same_topic edges
        by_topic = {}
        for n in nodes:
            if n.get("topic"):
                by_topic.setdefault(n["topic"], []).append(n["id"])
        for topic, ids in by_topic.items():
            for i in range(len(ids)):
                for j in range(i + 1, len(ids)):
                    conn.execute("""
                        INSERT OR IGNORE INTO memory_edges (bot_id, src, dst, kind, weight, metadata, created_at)
                        VALUES (?, ?, ?, 'same_topic', 1.0, '{}', ?)
                    """, (bot_id, ids[i], ids[j], now))
                    edges_added += 1

        # same_subject edges
        by_subject = {}
        for n in nodes:
            if n.get("subject"):
                by_subject.setdefault(n["subject"], []).append(n["id"])
        for subject, ids in by_subject.items():
            for i in range(len(ids)):
                for j in range(i + 1, len(ids)):
                    conn.execute("""
                        INSERT OR IGNORE INTO memory_edges (bot_id, src, dst, kind, weight, metadata, created_at)
                        VALUES (?, ?, ?, 'same_subject', 1.0, '{}', ?)
                    """, (bot_id, ids[i], ids[j], now))
                    edges_added += 1

        # same_day edges (capped at 50 per day to avoid explosion)
        by_day = {}
        for n in nodes:
            day = (n.get("captured_at") or "")[:10]
            if day:
                by_day.setdefault(day, []).append(n["id"])
        for day, ids in by_day.items():
            # Only create edges for small groups to avoid O(n^2) explosion
            if len(ids) > 50:
                continue
            for i in range(len(ids)):
                for j in range(i + 1, len(ids)):
                    conn.execute("""
                        INSERT OR IGNORE INTO memory_edges (bot_id, src, dst, kind, weight, metadata, created_at)
                        VALUES (?, ?, ?, 'same_day', 0.5, '{}', ?)
                    """, (bot_id, ids[i], ids[j], now))
                    edges_added += 1

        conn.commit()
        conn.close()
        return edges_added

    # --- Search ---

    def search(self, bot_id, query, limit=10):
        """Full-text search over memory nodes."""
        conn = self._conn()
        try:
            rows = conn.execute("""
                SELECT m.* FROM memory_nodes m
                JOIN memory_fts f ON m.rowid = f.rowid
                WHERE m.bot_id = ? AND memory_fts MATCH ?
                ORDER BY rank
                LIMIT ?
            """, (bot_id, query, limit)).fetchall()
        except sqlite3.OperationalError:
            # Fallback to LIKE search
            rows = conn.execute("""
                SELECT * FROM memory_nodes
                WHERE bot_id = ? AND (body LIKE ? OR subject LIKE ? OR topic LIKE ?)
                ORDER BY captured_at DESC
                LIMIT ?
            """, (bot_id, f"%{query}%", f"%{query}%", f"%{query}%", limit)).fetchall()
        conn.close()
        return [node_to_dict(r) for r in rows]

    def get_context(self, bot_id, query, limit=5):
        """Retrieve relevant memories for a query (for LLM context injection)."""
        nodes = self.search(bot_id, query, limit)
        if not nodes:
            return ""
        lines = ["Relevant memories:"]
        for n in nodes:
            conf = n.get("confidence", 1.0)
            layer = n.get("layer", "semantic")
            body = n.get("body", "")
            lines.append(f"  [{layer}] (conf={conf:.2f}) {body}")
        return "\n".join(lines)

    # --- LLM fact extraction (optional) ---

    def extract_facts(self, text, bot_id=DEFAULT_BOT_ID, model=None):
        """Extract facts from text using an LLM. Returns list of node dicts."""
        # Use 9router or a configured model
        api_base = os.environ.get("MEMORY_LLM_BASE_URL", "http://127.0.0.1:20128/v1")
        api_key = os.environ.get("MEMORY_LLM_API_KEY", os.environ.get("HERMES_CUSTOM_LOCALHOST_20128_API_KEY", ""))

        prompt = f"""Extract factual memories from the following text. For each fact, output a JSON object with:
- body: the fact statement (concise)
- layer: one of [episodic, semantic, procedural, profile]
- fact_type: one of [preference, fact, event, skill, goal, constraint]
- subject: main entity/topic
- topic: category
- confidence: 0.0-1.0

Output as a JSON array. Only include clear, durable facts. Skip transient or trivial statements.

Text:
{text}

JSON:"""

        try:
            data = json.dumps({
                "model": model or "auto",
                "messages": [{"role": "user", "content": prompt}],
                "temperature": 0.1,
            }).encode()
            req = urllib.request.Request(
                f"{api_base}/chat/completions",
                data=data,
                headers={"Content-Type": "application/json", "Authorization": f"Bearer {api_key}"},
            )
            with urllib.request.urlopen(req, timeout=60) as resp:
                result = json.loads(resp.read().decode())
            content = result["choices"][0]["message"]["content"]
            # Parse JSON from response
            # Find JSON array in response
            match = re.search(r'\[.*\]', content, re.DOTALL)
            if match:
                facts = json.loads(match.group())
            else:
                facts = []
        except Exception as e:
            facts = []
            print(f"LLM extraction failed: {e}", file=__import__("sys").stderr)

        nodes = []
        for f in facts:
            node = {
                "bot_id": bot_id,
                "body": f.get("body", ""),
                "layer": f.get("layer", "semantic"),
                "fact_type": f.get("fact_type", "fact"),
                "subject": f.get("subject"),
                "topic": f.get("topic"),
                "confidence": f.get("confidence", 0.8),
            }
            if node["body"]:
                nodes.append(self.upsert_node(node))
        return nodes


# --- HTTP API ---

store = MemoryStore()


class MemoryHandler(BaseHTTPRequestHandler):
    def _send_json(self, data, status=200):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data, default=str).encode())

    def _parse_body(self):
        length = int(self.headers.get("Content-Length", 0))
        if length > 0:
            return json.loads(self.rfile.read(length).decode())
        return {}

    def _bot_id(self):
        return self.headers.get("X-Bot-Id", DEFAULT_BOT_ID)

    def do_GET(self):
        parsed = urlparse(self.path)
        path = parsed.path
        params = parse_qs(parsed.query)

        if path == "/" or path == "/ui":
            ui_path = Path(__file__).parent / "memory_ui.html"
            if ui_path.exists():
                self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.end_headers()
                self.wfile.write(ui_path.read_bytes())
            else:
                self._send_json({"error": "UI not found"}, 404)
            return

        if path == "/health":
            self._send_json({"status": "ok", "service": "memory", "nodes": store.count_nodes(DEFAULT_BOT_ID)})
            return

        if path == "/v1/memory/nodes":
            bot_id = params.get("bot_id", [self._bot_id()])[0]
            layer = params.get("layer", [None])[0]
            topic = params.get("topic", [None])[0]
            subject = params.get("subject", [None])[0]
            limit = int(params.get("limit", ["100"])[0])
            nodes = store.list_nodes(bot_id, layer=layer, topic=topic, subject=subject, limit=limit)
            self._send_json({"nodes": nodes, "count": len(nodes)})
            return

        if path.startswith("/v1/memory/nodes/"):
            node_id = path.split("/")[-1]
            bot_id = params.get("bot_id", [self._bot_id()])[0]
            node = store.get_node(bot_id, node_id)
            if node:
                self._send_json(node)
            else:
                self._send_json({"error": "not found"}, 404)
            return

        if path == "/v1/memory/edges":
            bot_id = params.get("bot_id", [self._bot_id()])[0]
            edges = store.list_edges(bot_id)
            self._send_json({"edges": edges, "count": len(edges)})
            return

        if path == "/v1/memory/search":
            bot_id = params.get("bot_id", [self._bot_id()])[0]
            query = params.get("q", [""])[0]
            limit = int(params.get("limit", ["10"])[0])
            results = store.search(bot_id, query, limit)
            self._send_json({"results": results, "count": len(results)})
            return

        if path == "/v1/memory/context":
            bot_id = params.get("bot_id", [self._bot_id()])[0]
            query = params.get("q", [""])[0]
            limit = int(params.get("limit", ["5"])[0])
            ctx = store.get_context(bot_id, query, limit)
            self._send_json({"context": ctx})
            return

        if path == "/v1/memory/stats":
            bot_id = params.get("bot_id", [self._bot_id()])[0]
            self._send_json({
                "bot_id": bot_id,
                "nodes": store.count_nodes(bot_id),
                "edges": store.count_edges(bot_id),
            })
            return

        self._send_json({"error": "not found"}, 404)

    def do_POST(self):
        parsed = urlparse(self.path)
        path = parsed.path
        data = self._parse_body()

        if path == "/v1/memory/nodes":
            node = store.upsert_node(data)
            self._send_json(node, 201)
            return

        if path == "/v1/memory/edges":
            edges = data.get("edges", [data])
            store.upsert_edges(edges)
            self._send_json({"status": "ok", "count": len(edges)})
            return

        if path == "/v1/memory/search":
            bot_id = data.get("bot_id", self._bot_id())
            query = data.get("query", "")
            limit = data.get("limit", 10)
            results = store.search(bot_id, query, limit)
            self._send_json({"results": results, "count": len(results)})
            return

        if path == "/v1/memory/extract":
            bot_id = data.get("bot_id", self._bot_id())
            text = data.get("text", "")
            model = data.get("model")
            nodes = store.extract_facts(text, bot_id=bot_id, model=model)
            self._send_json({"extracted": len(nodes), "nodes": nodes})
            return

        if path == "/v1/memory/rebuild-edges":
            bot_id = data.get("bot_id", self._bot_id())
            count = store.rebuild_derived_edges(bot_id)
            self._send_json({"status": "ok", "edges_created": count})
            return

        if path == "/v1/memory/context":
            bot_id = data.get("bot_id", self._bot_id())
            query = data.get("query", "")
            limit = data.get("limit", 5)
            ctx = store.get_context(bot_id, query, limit)
            self._send_json({"context": ctx})
            return

        self._send_json({"error": "not found"}, 404)

    def do_DELETE(self):
        parsed = urlparse(self.path)
        path = parsed.path
        params = parse_qs(parsed.query)

        if path.startswith("/v1/memory/nodes/"):
            node_id = path.split("/")[-1]
            bot_id = params.get("bot_id", [self._bot_id()])[0]
            store.delete_node(bot_id, node_id)
            self._send_json({"status": "deleted", "id": node_id})
            return

        if path == "/v1/memory/nodes":
            bot_id = params.get("bot_id", [self._bot_id()])[0]
            store.delete_all_nodes(bot_id)
            self._send_json({"status": "deleted_all", "bot_id": bot_id})
            return

        if path == "/v1/memory/edges":
            bot_id = params.get("bot_id", [self._bot_id()])[0]
            store.delete_all_edges(bot_id)
            self._send_json({"status": "deleted_all_edges", "bot_id": bot_id})
            return

        self._send_json({"error": "not found"}, 404)

    def log_message(self, format, *args):
        pass


if __name__ == "__main__":
    init_db()
    print(f"Memory service starting on port {MEMORY_PORT}")
    print(f"Database: {MEMORY_DB}")
    server = HTTPServer(("0.0.0.0", MEMORY_PORT), MemoryHandler)
    server.serve_forever()
