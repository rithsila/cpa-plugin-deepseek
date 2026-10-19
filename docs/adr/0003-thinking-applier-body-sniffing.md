---
status: superseded by ADR-0004
---

# thinking.apply Dispatches by Body Shape

When the host invokes `thinking.apply`, it only provides `Provider`, `Model`, normalized `Config` (Mode/Budget/Level), and `Body`—**it does not provide the protocol format**. However, DeepSeek's three protocol surfaces use completely different thinking fields: `chat-completions` uses `thinking.type` + `reasoning_effort`, `anthropic` uses `reasoning.effort` + `output_config.effort`, and `responses` uses `reasoning.effort`.

Three possible approaches:
1. Rely on model metadata carrying protocol flags (not guaranteed in apply requests).
2. Sniff the request body shape.
3. Only populate missing fields without normalization.

We chose to sniff the body shape: having `input` implies `responses`; having top-level `system` and `max_tokens` implies `anthropic`; otherwise treat as `chat-completions`. Structural differences between the three payloads are large enough that a misclassification writes to a non-existent field rather than corrupting an existing one—a manageable risk verifiable in unit tests. The trade-off is hardcoding this detection inside `thinking.apply` rather than extracting the format from the request context.
