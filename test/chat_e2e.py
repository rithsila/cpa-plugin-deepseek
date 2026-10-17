#!/usr/bin/env python3
"""Verifies the chat-completions surface end to end against the running host.

What this script proves:
  - a thinking-enabled request returns reasoning_content, and the same prompt
    with `thinking.type=disabled` does not (so the toggle reaches upstream);
  - the upstream usage payload survives, both non-streaming and in the final
    streaming chunk;
  - a streaming response ends with the upstream `[DONE]` and never carries the
    plugin's synthesized failure chunk.

What it does not prove: that `reasoning_effort` is forwarded. Effort is not
reliably observable in model output, so that claim rests on the byte-fidelity
test at the host-callback seam, not here.
"""
import json
import sys
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:18317"
MODEL = "deepseek-cpa/deepseek-v4-pro"
PROMPT = "9.11 and 9.8, which is greater? Answer in one short sentence."


def call(path, payload, accept="application/json"):
    request = urllib.request.Request(
        BASE + path,
        data=json.dumps(payload).encode(),
        headers={"content-type": "application/json", "accept": accept},
    )
    try:
        with urllib.request.urlopen(request, timeout=180) as response:
            return response.status, response.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", "replace")


def require_completion(raw):
    """Returns (body, message) and fails loudly on an empty or wrong-shaped reply."""
    body = json.loads(raw)
    choices = body.get("choices")
    if not isinstance(choices, list) or not choices:
        raise AssertionError(f"response carries no choices: {raw[:200]}")
    message = choices[0].get("message")
    if not isinstance(message, dict) or not message.get("content"):
        raise AssertionError(f"response carries no assistant content: {raw[:200]}")
    usage = body.get("usage")
    if not isinstance(usage, dict) or not usage.get("completion_tokens"):
        raise AssertionError(f"response carries no usable usage counts: {raw[:200]}")
    return message, usage


def main():
    failures = []

    # Same prompt, same model: only the thinking toggle differs.
    status, raw = call("/v1/chat/completions", {
        "model": MODEL,
        "messages": [{"role": "user", "content": PROMPT}],
        "thinking": {"type": "enabled"},
        "reasoning_effort": "high",
    })
    print(f"thinking on: status={status}")
    thinking_on = None
    if status != 200:
        print("  body:", raw[:300])
        failures.append(f"thinking-enabled request failed with {status}")
    else:
        try:
            message, usage = require_completion(raw)
            thinking_on = message.get("reasoning_content") or ""
            print(f"  content_chars={len(message['content'])} "
                  f"reasoning_chars={len(thinking_on)} completion_tokens={usage['completion_tokens']}")
            if not thinking_on:
                failures.append("thinking.type=enabled produced no reasoning_content")
        except AssertionError as exc:
            failures.append(str(exc))

    status, raw = call("/v1/chat/completions", {
        "model": MODEL,
        "messages": [{"role": "user", "content": PROMPT}],
        "thinking": {"type": "disabled"},
    })
    print(f"thinking off: status={status}")
    if status != 200:
        print("  body:", raw[:300])
        failures.append(f"thinking-disabled request failed with {status}")
    else:
        try:
            message, _ = require_completion(raw)
            reasoning_off = message.get("reasoning_content") or ""
            print(f"  content_chars={len(message['content'])} reasoning_chars={len(reasoning_off)}")
            if reasoning_off:
                failures.append("thinking.type=disabled still produced reasoning_content")
            if thinking_on and message["content"] == thinking_on:
                failures.append("both arms returned identical content; the comparison proves nothing")
        except AssertionError as exc:
            failures.append(str(exc))

    status, raw = call("/v1/chat/completions", {
        "model": MODEL,
        "messages": [{"role": "user", "content": "count from 1 to 5"}],
        "stream": True,
        "stream_options": {"include_usage": True},
    }, accept="text/event-stream")
    frames = [line[len("data:"):].strip() for line in raw.splitlines() if line.startswith("data:")]
    print(f"stream: status={status} data_frames={len(frames)}")
    if status != 200:
        print("  body:", raw[:300])
        failures.append(f"streaming request failed with {status}")
    else:
        print(f"  last={frames[-1] if frames else '(none)'}")
        if not frames or frames[-1] != "[DONE]":
            failures.append("stream did not end with [DONE]")
        if any("upstream_stream_error" in frame for frame in frames):
            failures.append("stream carried the plugin's synthesized failure chunk")

        usage_counts = []
        for frame in frames:
            if frame == "[DONE]" or not frame:
                continue
            try:
                body = json.loads(frame)
            except json.JSONDecodeError:
                failures.append(f"stream carried a non-JSON data frame: {frame[:80]!r}")
                continue
            usage = body.get("usage")
            if isinstance(usage, dict) and usage.get("completion_tokens"):
                usage_counts.append(usage["completion_tokens"])
        print(f"  usage_frames={len(usage_counts)}")
        if not usage_counts:
            failures.append("stream carried no usable usage payload")

    if failures:
        for failure in failures:
            print("FAIL:", failure)
        return 1
    print("chat surface checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
