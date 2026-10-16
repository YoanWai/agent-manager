#!/usr/bin/env python3
"""Raw MCP stdio probe: newline-delimited JSON-RPC against `agent-manager mcp`.

No model call, no approval: initialize, tools/list, and one optional
tools/call. Prints a JSON result so matrix.sh can file it as evidence.

Usage: mcp_probe.py <binary> <home> <protocolVersion> [toolName]
"""
import json
import os
import selectors
from pathlib import Path
import subprocess
import sys


def probe(binary, home, protocol, tool=None, caller=None):
    env = dict(os.environ)
    Path(home).mkdir(parents=True, exist_ok=True)
    env["HOME"] = home
    env["XDG_CONFIG_HOME"] = str(Path(home) / ".config")
    env["XDG_DATA_HOME"] = str(Path(home) / ".local/share")
    env["XDG_STATE_HOME"] = str(Path(home) / ".local/state")
    for key in ("TMUX", "TMUX_PANE", "TMUX_TMPDIR", "AGENT_MANAGER_SESSION_ID"):
        env.pop(key, None)
    if caller:
        env["AGENT_MANAGER_SESSION_ID"] = caller
    proc = subprocess.Popen(
        [binary, "mcp"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
        text=True,
    )

    def send(obj):
        proc.stdin.write(json.dumps(obj) + "\n")
        proc.stdin.flush()

    def recv():
        with selectors.DefaultSelector() as selector:
            selector.register(proc.stdout, selectors.EVENT_READ)
            if not selector.select(timeout=15):
                raise TimeoutError("MCP response deadline")
        line = proc.stdout.readline()
        if not line:
            proc.wait(timeout=5)
            raise RuntimeError("server closed early: " + proc.stderr.read()[:2000])
        return json.loads(line)

    out = {"requested": protocol}
    try:
        send({
            "jsonrpc": "2.0", "id": 1, "method": "initialize",
            "params": {
                "protocolVersion": protocol,
                "capabilities": {},
                "clientInfo": {"name": "compat-probe", "version": "0"},
            },
        })
        init = recv()
        if "error" in init:
            return {**out, "negotiated": None, "initialize_error": init["error"]}
        out["negotiated"] = init["result"]["protocolVersion"]
        out["serverInfo"] = init["result"].get("serverInfo")
        send({"jsonrpc": "2.0", "method": "notifications/initialized"})
        send({"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}})
        tools = recv()
        if "result" in tools:
            out["tool_count"] = len(tools["result"].get("tools", []))
            out["tools"] = [t["name"] for t in tools["result"].get("tools", [])]
        else:
            out["tools_error"] = tools.get("error")
        if tool:
            send({
                "jsonrpc": "2.0", "id": 3, "method": "tools/call",
                "params": {"name": tool, "arguments": {}},
            })
            called = recv()
            out["call"] = called
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)
    return out


if __name__ == "__main__":
    if len(sys.argv) < 4:
        sys.exit(__doc__)
    print(json.dumps(probe(sys.argv[1], sys.argv[2], sys.argv[3],
                           sys.argv[4] if len(sys.argv) > 4 else None), indent=1))
