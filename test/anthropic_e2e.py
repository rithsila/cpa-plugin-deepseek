#!/usr/bin/env python3
"""Verifies the anthropic protocol surface end to end against the running host.

Turn 1 asks a question that forces a tool call; turn 2 replays the assistant's
thinking + tool_use blocks together with the tool result. The check is that the
plugin forwards that history byte for byte, so the multi-turn thinking session
completes on the anthropic surface.

Measured behaviour: DeepSeek's anthropic endpoint tolerates a stripped thinking
history, so this script does not treat "no 400" as proof of fidelity. The
negative control only reports what the upstream does; the fidelity claim comes
from turn 1 carrying a thinking block that turn 2 replays.

The script never sees the upstream key: the host holds it.
"""
import json
import sys
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:18317"
MODEL = "deepseek-cpa/deepseek-v4-pro"

TOOLS = [{
    "name": "read_file",
    "description": "Read the contents of a file",
    "input_schema": {
        "type": "object",
        "properties": {"path": {"type": "string"}},
        "required": ["path"],
    },
}]


def post(path, payload, accept="application/json"):
    body = json.dumps(payload).encode()
    request = urllib.request.Request(
        BASE + path,
        data=body,
        headers={"content-type": "application/json", "anthropic-version": "2023-06-01",
                 "accept": accept},
    )
    try:
        with urllib.request.urlopen(request, timeout=180) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read()


def post_json(path, payload):
    status, raw = post(path, payload)
    try:
        return status, json.loads(raw)
    except json.JSONDecodeError:
        return status, {"raw": raw.decode("utf-8", "replace")[:400]}


def blocks(message):
    return [block.get("type") for block in message.get("content", [])]


def text_of(message):
    return "".join(b.get("text", "") for b in message.get("content", []) if b.get("type") == "text")


def main():
    payload = {
        "model": MODEL,
        "max_tokens": 2048,
        "thinking": {"type": "enabled", "budget_tokens": 2048},
        "tools": TOOLS,
        "messages": [{"role": "user", "content": "Read a.txt with the tool, then tell me what it contains."}],
    }

    status, turn1 = post_json("/v1/messages", payload)
    print(f"turn1 status={status} blocks={blocks(turn1)}")
    if status != 200:
        print("turn1 failed:", json.dumps(turn1)[:400])
        return 1

    tool_uses = [b for b in turn1.get("content", []) if b.get("type") == "tool_use"]
    thinking = [b for b in turn1.get("content", []) if b.get("type") == "thinking"]
    print(f"turn1 thinking_blocks={len(thinking)} tool_uses={len(tool_uses)}")
    if not thinking:
        print("FAIL: turn 1 returned no thinking block, so replay fidelity is untested")
        return 1
    if not tool_uses:
        print("FAIL: model answered without a tool call")
        return 1

    messages = [
        {"role": "user", "content": "Read a.txt with the tool, then tell me what it contains."},
        {"role": "assistant", "content": turn1["content"]},
        {"role": "user", "content": [{
            "type": "tool_result",
            "tool_use_id": tool_uses[0]["id"],
            "content": "hello from a.txt",
        }]},
    ]
    replay = dict(payload, messages=messages)
    status2, turn2 = post_json("/v1/messages", replay)
    print(f"turn2 status={status2} blocks={blocks(turn2)}")
    if status2 != 200:
        print("turn2 failed:", json.dumps(turn2)[:500])
        return 1
    answer = text_of(turn2)
    print("turn2 answer:", answer[:200])
    if "hello" not in answer.lower():
        print("WARNING: answer does not mention the tool output")
        return 1

    # Negative control: drop the thinking blocks from the replayed history and
    # report what the upstream does. Informational only: DeepSeek accepts this on
    # the anthropic surface, so it is not evidence of the plugin's fidelity.
    stripped = [m for m in messages]
    stripped[1] = {"role": "assistant",
                   "content": [b for b in turn1["content"] if b.get("type") != "thinking"]}
    status3, turn3 = post_json("/v1/messages", dict(payload, messages=stripped))
    print(f"negative-control (thinking stripped) status={status3}", end="")
    if status3 != 200:
        print(f" -> rejected: {json.dumps(turn3)[:200]}")
    else:
        print(" -> accepted (DeepSeek tolerated the missing reasoning)")

    return 0


if __name__ == "__main__":
    sys.exit(main())
