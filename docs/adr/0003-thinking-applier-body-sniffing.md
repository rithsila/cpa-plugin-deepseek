# thinking.apply 靠 body 形状分发

宿主调用 `thinking.apply` 时只给 `Provider`、`Model`、已归一的 `Config`（Mode/Budget/Level）和 `Body`，**不给协议格式**。而 DeepSeek 三个协议面的 thinking 字段完全不同：`chat-completions` 用 `thinking.type` + `reasoning_effort`，`anthropic` 用 `reasoning.effort` + `output_config.effort`，`responses` 用 `reasoning.effort`。

三个可选做法：靠 model 元数据带协议标记（apply 请求不保证有这个字段）、靠嗅探 body 形状、只补缺失字段不做归一化。

选择嗅探 body 形状：有 `input` 即 `responses`，有顶层 `system` 且有 `max_tokens` 即 `anthropic`，其余按 `chat-completions`。三种载荷的结构差异足够大，判错时会写进不存在的字段而不是写进错的字段，属于能在测试里锁住的可控风险。代价是这个判定必须写死在 `thinking.apply` 里，不能像其他能力面那样按请求上下文取格式。
