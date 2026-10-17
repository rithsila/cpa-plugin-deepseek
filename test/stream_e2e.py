#!/usr/bin/env python3
"""Captures real SSE from both protocol surfaces and reports the event sequence.

The point is the terminal event: a responses stream must end with
response.completed and must NOT gain the plugin's synthesized response.failed,
and an anthropic stream must reach message_stop.
"""
import json
import sys
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:18317"


def stream(path, payload, headers):
    request = urllib.request.Request(
        BASE + path,
        data=json.dumps(payload).encode(),
        headers={"content-type": "application/json", "accept": "text/event-stream", **headers},
    )
    try:
        with urllib.request.urlopen(request, timeout=180) as response:
            return response.status, response.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", "replace")


def events_of(raw):
    """Returns (event names, frame count) without re-parsing payloads."""
    names = []
    frames = 0
    for line in raw.splitlines():
        if line.startswith("event:"):
            names.append(line[len("event:"):].strip())
        elif line.startswith("data:"):
            frames += 1
            if line.strip() == "data: [DONE]":
                names.append("[DONE]")
    return names, frames


def main():
    failures = []

    status, raw = stream("/v1/messages", {
        "model": "deepseek-cpa/deepseek-v4-pro",
        "max_tokens": 256,
        "stream": True,
        "messages": [{"role": "user", "content": "count from 1 to 5"}],
    }, {"anthropic-version": "2023-06-01"})
    names, frames = events_of(raw)
    print(f"anthropic stream status={status} frames={frames}")
    print("  events:", ", ".join(names[:6]), "..." if len(names) > 6 else "")
    print("  last:", names[-1] if names else "(none)")
    if status != 200:
        failures.append(f"anthropic stream status {status}: {raw[:200]}")
    elif not names or names[-1] != "message_stop":
        failures.append(f"anthropic stream did not end with message_stop (last={names[-1:]})")
    elif "error" in names:
        failures.append("anthropic stream carried an error event")

    status, raw = stream("/v1/responses", {
        "model": "deepseek-cpa/deepseek-v4-pro",
        "input": "count from 1 to 5",
        "stream": True,
    }, {})
    names, frames = events_of(raw)
    print(f"responses stream status={status} frames={frames}")
    print("  events:", ", ".join(names[:6]), "..." if len(names) > 6 else "")
    print("  last:", names[-1] if names else "(none)")
    if status != 200:
        failures.append(f"responses stream status {status}: {raw[:200]}")
    elif "response.failed" in names:
        failures.append("responses stream ended in the synthesized response.failed")
    elif not names or names[-1] != "response.completed":
        failures.append(f"responses stream did not end with response.completed (last={names[-1:]})")

    if failures:
        for failure in failures:
            print("FAIL:", failure)
        return 1
    print("streaming checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
