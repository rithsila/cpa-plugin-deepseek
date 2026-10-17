# 转发流时按 SSE 行边界重切 chunk，保证整行一次送达

流式转发最初把上游 chunk 原样交给宿主，包括 chunk 边界。这会丢掉 usage，而且只在部分请求上丢。

## 现象

宿主记下 `request usage` 时，`input_tokens`/`output_tokens` 常为 0，但同一条日志里 `ttft_ms`、`latency_ms`、`response_model` 都正常。实测在 Codex 客户端走的 `GET /backend-api/codex/responses` 上，61 次请求只有 6 次带 token；同一端点下其他插件的覆盖率是 96%–100%。

## 原因

DeepSeek 的 `response.completed` 帧会把 `instructions` 和 `tools` 原样回显，单行 JSON 达 77KB。宿主把这些 plugin chunk 拼成 SSE 行，但缓冲有硬上限（`internal/pluginhost/adapters_executors.go`，`maxLineBufferSize = 64*1024`）；一旦在读到换行前超过上限，它把半截缓冲交给用量观察器，然后**连同剩余部分一起丢弃**：

```go
if len(lineBuffer) > maxLineBufferSize {
    helps.ObservePluginExecutorStreamUsage(..., lineBuffer, &streamUsage)
    lineBuffer = nil
}
```

半截 JSON 解析失败，后半截再无机会拼回，usage 就此丢失。是否触发取决于上游 chunk 边界恰好有没有把这行切开——所以表现为间歇性丢失，而不是稳定失败。

在插件里按同一算法重放整条流可以直接区分两种情况：`term_complete=false` 且 `flushes=1` 的请求宿主记 0，`term_complete=true` 的请求正常。

## 决定

responses 与 anthropic 协议面上，插件把上游字节按 SSE 换行边界重新切 chunk，每条完整行（含换行符）在一个 chunk 内发出；流结束时若还剩不完整尾行，单独发出。

字节内容、顺序、帧结构都不变，只是分块边界变了。宿主在同一轮循环里就能把这行消费掉，缓冲区随即清空，64KB 上限不再有机会触发。

chat 协议面不动：宿主自己给该面组帧，插件仍然只发裸 JSON 载荷并丢弃上游的 `[DONE]`。

修复后实测连续 7 次请求全部带 token，0 次丢失。

## 为什么不改用别的手段

- **减小终态帧**：帧大是因为上游回显 `instructions`/`tools`。删字段会破坏协议对齐直通（ADR-0001），也让客户端拿不到它自己发过的内容。
- **拆成多条 `data:` 行**：SSE 允许一个事件的数据跨多条 `data:` 行由客户端拼接，但宿主的用量解析逐行读 JSON，拆行等于让宿主继续读不到。
- **只修宿主**：`lineBuffer = nil` 丢弃数据确实是宿主的问题，值得上游修复；但插件无法等待上游发版，而行对齐在插件侧就能消除触发条件。

行对齐不依赖宿主实现细节里的具体数值：即使上限改成别的值，整行一次送达也不会触发任何「读到换行前先攒着」的分支。
