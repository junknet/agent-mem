import json
import os
import signal
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path

BASE_URL = "http://127.0.0.1:8787"
DATABASE_URL = os.environ.get(
    "DATABASE_URL",
    "postgresql://cortex:cortex_password_secure@localhost:5440/cortex_knowledge",
)
GO_WORKDIR = Path(__file__).resolve().parent.parent / "mcp-go"

MACHINE_NAME = "test-machine"
PROJECT_PATH = "/tmp/agent-mem-e2e"

CONTENT_V1 = """# 数据库选型
我们决定使用 PostgreSQL + pgvector 作为主存储。
原因：
1. 支持向量检索
2. 生态成熟
"""

CONTENT_V2 = """# 数据库选型
最终采用 PostgreSQL + pgvector 作为主存储。
原因：
1. 支持向量检索
2. 生态成熟
3. 便于扩展
"""


def http_request(method, path, params=None, body=None, timeout=10):
    url = BASE_URL + path
    if params:
        url += "?" + urllib.parse.urlencode(params)
    data = None
    headers = {}
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return response.status, response.read().decode("utf-8")


def start_server():
    env = os.environ.copy()
    env["AGENT_MEM_LLM_MODE"] = "mock"
    env["AGENT_MEM_EMBEDDING_PROVIDER"] = "mock"
    env["DATABASE_URL"] = DATABASE_URL
    cmd = [
        "go",
        "run",
        "./cmd/agent-mem-mcp",
        "--reset-db",
        "--host",
        "127.0.0.1",
        "--port",
        "8787",
    ]
    process = subprocess.Popen(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        env=env,
        cwd=str(GO_WORKDIR),
        preexec_fn=os.setsid,
    )

    for _ in range(12):
        time.sleep(1)
        try:
            status, _ = http_request(
                "GET",
                "/projects",
                params={"machine_name": MACHINE_NAME, "limit": 1},
                timeout=2,
            )
            if status == 200:
                return process
        except Exception:
            continue

    print("❌ 服务启动失败")
    stop_server(process)
    return None


def stop_server(process):
    if not process:
        return
    try:
        os.killpg(os.getpgid(process.pid), signal.SIGTERM)
    except ProcessLookupError:
        return
    stdout, stderr = process.communicate(timeout=5)
    if stdout:
        print("--- STDOUT ---")
        print(stdout)
    if stderr:
        print("--- STDERR ---")
        print(stderr)


def ingest(content):
    payload = {
        "machine_name": MACHINE_NAME,
        "project_path": PROJECT_PATH,
        "content_type": "development",  # 新分类：requirement|plan|development|testing|insight
        "content": content,
        "ts": int(time.time()),
    }
    status, body = http_request("POST", "/ingest/memory", body=payload)
    data = json.loads(body)
    return status, data


def main():
    print("🚀 启动 Go 服务...")
    server = start_server()
    if not server:
        sys.exit(1)

    try:
        print("\n[1] 初次写入")
        status, data = ingest(CONTENT_V1)
        print(status, data)
        if status != 200 or data.get("status") != "created":
            print("❌ 初次写入失败")
            sys.exit(1)

        print("\n[2] 相同内容重复写入")
        status, data = ingest(CONTENT_V1)
        print(status, data)
        # LLM 仲裁：完全相同内容应返回 skipped（mock 向量可能不命中，返回 created 也允许）
        if status != 200:
            print("❌ 重复写入请求失败")
            sys.exit(1)
        if data.get("status") not in ("skipped", "created"):
            print(f"⚠️ 重复内容状态: {data.get('status')}（预期 skipped 或 created）")

        print("\n[3] 语义更新")
        status, data = ingest(CONTENT_V2)
        print(status, data)
        if status != 200:
            print("❌ 语义更新失败")
            sys.exit(1)
        if data.get("status") not in ("updated", "created"):
            print("❌ 语义更新状态异常")
            sys.exit(1)
        if data.get("status") == "created":
            print("⚠️ 语义更新未命中（mock 向量下允许）")

        print("\n[4] 语义检索")
        status, body = http_request(
            "GET",
            "/memories/search",
            params={
                "machine_name": MACHINE_NAME,
                "project_path": PROJECT_PATH,
                "query": "为什么选择 PostgreSQL",
                "scope": "development",  # 新分类
                "limit": 5,
            },
        )
        print(status, body)
        if status != 200:
            print("❌ 检索失败")
            sys.exit(1)
        search_data = json.loads(body)
        results = search_data.get("results", [])
        if not results:
            print("❌ 检索无结果")
            sys.exit(1)

        memory_id = results[0]["id"]

        print("\n[5] 获取完整内容")
        status, body = http_request("GET", "/memories", params={"ids": memory_id})
        print(status, body)
        if status != 200:
            print("❌ 获取失败")
            sys.exit(1)
        get_data = json.loads(body)
        if not get_data.get("results"):
            print("❌ 获取无结果")
            sys.exit(1)

        print("\n✅ E2E 测试完成")
    finally:
        stop_server(server)


if __name__ == "__main__":
    main()
