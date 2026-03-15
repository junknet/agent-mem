#!/usr/bin/env python3
"""E2E test for MCP transports (Streamable HTTP + SSE).

Usage:
    python scripts/test_mcp_transports.py [--host HOST] [--port PORT] [--token TOKEN]

Tests both /mcp (Streamable HTTP) and /sse (SSE) endpoints.
"""

import argparse
import http.client
import json
import queue
import sys
import threading
import time


def test_streamable_http(host: str, port: int, token: str) -> bool:
    """Test /mcp Streamable HTTP transport with proper Mcp-Session-Id header."""
    print("\n=== Streamable HTTP (/mcp) ===")

    base_path = f"/mcp?token={token}"
    headers_base = {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    }
    session_id = None

    def post(payload: dict, sid: str | None = None) -> tuple[int, dict | None, str | None]:
        conn = http.client.HTTPConnection(host, port, timeout=15)
        h = dict(headers_base)
        if sid:
            h["Mcp-Session-Id"] = sid
        conn.request("POST", base_path, json.dumps(payload).encode(), h)
        resp = conn.getresponse()
        body = resp.read().decode()
        resp_sid = resp.getheader("Mcp-Session-Id")
        conn.close()
        for line in body.split("\n"):
            if line.startswith("data: "):
                return resp.status, json.loads(line[6:]), resp_sid
        try:
            return resp.status, json.loads(body), resp_sid
        except Exception:
            return resp.status, None, resp_sid

    # 1. Initialize
    status, data, session_id = post({
        "jsonrpc": "2.0", "id": 1, "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "test", "version": "1.0"},
        },
    })
    if status != 200 or not session_id:
        print(f"  FAIL: initialize returned {status}, sid={session_id}")
        return False
    print(f"  init OK, session={session_id[:12]}...")

    # 2. Notification (must use Mcp-Session-Id header)
    status2, _, _ = post(
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        sid=session_id,
    )
    if status2 not in (200, 202, 204):
        print(f"  FAIL: notification returned {status2}")
        return False
    print(f"  notification OK ({status2})")

    time.sleep(0.3)

    # 3. tools/list
    status3, data3, _ = post(
        {"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
        sid=session_id,
    )
    if status3 != 200 or not data3 or "error" in data3:
        err_msg = data3.get("error", {}).get("message", "") if data3 else "no response"
        print(f"  FAIL: tools/list: {err_msg}")
        return False
    tools = data3.get("result", {}).get("tools", [])
    print(f"  tools/list OK: {len(tools)} tools")

    # 4. tools/call — mem.search
    status4, data4, _ = post(
        {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {
            "name": "mem.search",
            "arguments": {
                "query": "test", "owner_id": "personal", "scope": "all",
                "project_key": "test", "project_name": "test", "limit": 5,
            },
        }},
        sid=session_id,
    )
    if status4 != 200 or not data4 or "error" in data4:
        err_msg = data4.get("error", {}).get("message", "") if data4 else "no response"
        print(f"  FAIL: mem.search: {err_msg}")
        return False
    print("  mem.search OK")

    print("  PASS: Streamable HTTP")
    return True


def test_sse(host: str, port: int, token: str) -> bool:
    """Test /sse SSE transport."""
    print("\n=== SSE (/sse) ===")

    result_queue: queue.Queue = queue.Queue()
    endpoint = None
    endpoint_event = threading.Event()

    def sse_listener():
        nonlocal endpoint
        conn = http.client.HTTPConnection(host, port, timeout=30)
        conn.request("GET", f"/sse?token={token}", headers={"Accept": "text/event-stream"})
        resp = conn.getresponse()
        event_type = None
        while True:
            line = resp.readline().decode().rstrip("\n")
            if line.startswith("event: "):
                event_type = line[7:]
            elif line.startswith("data: "):
                data = line[6:]
                if event_type == "endpoint":
                    endpoint = data
                    endpoint_event.set()
                elif event_type == "message":
                    try:
                        result_queue.put(json.loads(data))
                    except Exception:
                        result_queue.put({"raw": data})
                event_type = None

    t = threading.Thread(target=sse_listener, daemon=True)
    t.start()
    if not endpoint_event.wait(timeout=10):
        print("  FAIL: no SSE endpoint received")
        return False
    print(f"  SSE endpoint: {endpoint[:40]}...")

    msg_id = 0

    def post_msg(payload: dict) -> dict | None:
        nonlocal msg_id
        conn = http.client.HTTPConnection(host, port, timeout=15)
        url = f"{endpoint}&token={token}" if "?" in endpoint else f"{endpoint}?token={token}"
        conn.request("POST", url, json.dumps(payload).encode(), {"Content-Type": "application/json"})
        resp = conn.getresponse()
        resp.read()
        conn.close()
        if "id" in payload:
            try:
                return result_queue.get(timeout=15)
            except queue.Empty:
                return {"timeout": True}
        return None

    # 1. Initialize
    msg_id += 1
    r = post_msg({
        "jsonrpc": "2.0", "id": msg_id, "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "test-sse", "version": "1.0"},
        },
    })
    if not r or "result" not in r:
        print(f"  FAIL: initialize: {r}")
        return False
    print("  init OK")

    # 2. Notification
    post_msg({"jsonrpc": "2.0", "method": "notifications/initialized"})
    time.sleep(0.3)

    # 3. tools/list
    msg_id += 1
    r2 = post_msg({"jsonrpc": "2.0", "id": msg_id, "method": "tools/list"})
    if not r2 or "result" not in r2:
        err = r2.get("error", {}).get("message", "") if r2 else "timeout"
        print(f"  FAIL: tools/list: {err}")
        return False
    tools = r2["result"].get("tools", [])
    print(f"  tools/list OK: {len(tools)} tools")

    # 4. mem.search
    msg_id += 1
    r3 = post_msg({
        "jsonrpc": "2.0", "id": msg_id, "method": "tools/call",
        "params": {
            "name": "mem.search",
            "arguments": {
                "query": "test", "owner_id": "personal", "scope": "all",
                "project_key": "test", "project_name": "test", "limit": 5,
            },
        },
    })
    if not r3 or "error" in r3:
        err = r3.get("error", {}).get("message", "") if r3 else "timeout"
        print(f"  FAIL: mem.search: {err}")
        return False
    print("  mem.search OK")

    # 5. mem.ingest_memory
    msg_id += 1
    r4 = post_msg({
        "jsonrpc": "2.0", "id": msg_id, "method": "tools/call",
        "params": {
            "name": "mem.ingest_memory",
            "arguments": {
                "owner_id": "personal",
                "project_key": "_test_transport",
                "project_name": "Transport Test",
                "project_path": "/tmp/test",
                "content": f"Transport test at {time.strftime('%Y-%m-%d %H:%M:%S')}",
                "content_type": "testing",
                "tags": ["_test", "transport"],
                "ts": int(time.time()),
            },
        },
    })
    if not r4 or "error" in r4:
        err = r4.get("error", {}).get("message", "") if r4 else "timeout"
        print(f"  FAIL: mem.ingest: {err}")
        return False
    print("  mem.ingest OK")

    print("  PASS: SSE")
    return True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8787)
    parser.add_argument("--token", default="")
    args = parser.parse_args()

    results = {}
    results["streamable_http"] = test_streamable_http(args.host, args.port, args.token)
    results["sse"] = test_sse(args.host, args.port, args.token)

    print("\n=== Summary ===")
    all_pass = True
    for name, passed in results.items():
        status = "PASS" if passed else "FAIL"
        print(f"  {name}: {status}")
        if not passed:
            all_pass = False

    sys.exit(0 if all_pass else 1)


if __name__ == "__main__":
    main()
