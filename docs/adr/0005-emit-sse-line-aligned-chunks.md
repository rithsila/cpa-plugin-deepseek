# Re-frame Stream Chunks on SSE Newline Boundaries

Stream forwarding initially forwarded upstream chunks verbatim to the host, including upstream chunk boundaries. This led to token usage data being dropped intermittently on specific requests.

## Symptom

When the host logged `request usage`, `input_tokens` and `output_tokens` were often 0, even though `ttft_ms`, `latency_ms`, and `response_model` were normal. In testing against `GET /backend-api/codex/responses` with a Codex client, only 6 out of 61 requests contained token counts; other plugins on the same endpoint achieved 96%–100% coverage.

## Root Cause

DeepSeek's `response.completed` frame echoes instructions and tools back, resulting in a single SSE line reaching up to 77KB. The host assembles plugin chunks into lines but enforces a hard cap on buffer size (`internal/pluginhost/adapters_executors.go`, `maxLineBufferSize = 64*1024`). Once the buffer exceeds this limit before encountering a newline, it feeds the partial buffer to the usage observer and **discards the buffer along with the remainder of the line**:

```go
if len(lineBuffer) > maxLineBufferSize {
    helps.ObservePluginExecutorStreamUsage(..., lineBuffer, &streamUsage)
    lineBuffer = nil
}
```

Parsing the truncated JSON fails, the remainder of the line is never reassembled, and usage data is permanently lost. Whether this occurs depends on whether an upstream chunk boundary split that 77KB line across chunks—explaining the intermittent behavior.

Replaying streams within the plugin confirmed this: requests with `term_complete=false` and `flushes=1` logged 0 tokens on the host, while requests with `term_complete=true` logged correctly.

## Decision

On the `responses` and `anthropic` surfaces, the plugin re-frames upstream bytes on SSE newline boundaries, emitting each complete line (including newline) within a single chunk. Any trailing incomplete frame left when the stream ends is emitted separately.

Payload content, ordering, and SSE frame structures remain unchanged; only chunk boundaries change. The host consumes the complete line in a single pass, clearing its buffer immediately and preventing the 64KB cap from triggering.

The `chat` surface remains unchanged: the host frames chat chunks itself, so the plugin continues emitting bare JSON payloads and stripping upstream `[DONE]`.

After this fix, 7 consecutive test requests all recorded full token counts with 0 drops.

## Why Other Alternatives Were Rejected

- **Truncating terminal frames**: The frame is large because upstream echoes `instructions`/`tools`. Removing fields violates protocol-aligned passthrough (ADR-0001) and deprives clients of their submitted context.
- **Splitting across multiple `data:` lines**: While SSE permits data across multiple `data:` lines, the host parses JSON line-by-line; splitting lines prevents the host from reading the usage object.
- **Fixing host only**: Discarding buffers upon reaching `maxLineBufferSize` is an upstream host bug worth addressing; however, the plugin cannot wait for a host release, and line alignment in the plugin eliminates the trigger condition immediately.

Line alignment does not depend on specific buffer thresholds: even if the host changes `maxLineBufferSize`, delivering whole lines avoids buffering incomplete lines across read cycles.
