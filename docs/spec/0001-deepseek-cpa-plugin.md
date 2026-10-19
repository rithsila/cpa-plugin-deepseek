# deepseek-cpa: Native DeepSeek Plugin for CLIProxyAPI

## Problem Statement

Operators encounter three categories of problems when using CLIProxyAPI to proxy DeepSeek.

**1. Sessions break completely.** DeepSeek thinking models with `tools` require all past `reasoning_content` turns to be replayed verbatim. Omitting even a single turn returns an immediate HTTP 400 error. The built-in `openai-compatibility` channel follows a conversion pipeline: `client protocol → internal representation → upstream protocol`. Any field dropped by intermediate translation shifts this constraint from "graceful degradation" into a hard break. Anthropic clients like Claude Code are especially vulnerable: their protocol binds reasoning blocks and tool calls together; converting them risks misalignment.

**2. Thinking parameters are incompatible across the three protocols.** DeepSeek uses `thinking.type` and `reasoning_effort` in `chat-completions`, `reasoning.effort` and `output_config.effort` in `anthropic`, and `reasoning.effort` in `responses`. The built-in channel does not recognize these differences, forcing clients to guess which field will take effect.

**3. Operational overhead is pushed outward.** Managing multiple API keys, credential sources, and token usage tracking requires building separate external systems.

## Solution

A native CLIProxyAPI plugin with the identifier `deepseek-cpa`.

Clients connect using their preferred native protocol and specify this plugin using `deepseek-cpa/<upstream-model-name>`. The plugin forwards the **original client request body** to DeepSeek's matching protocol endpoint—`chat-completions` and `responses` target `https://api.deepseek.com`, while `anthropic` targets `https://api.deepseek.com/anthropic`—rewriting only the model name. Because there is no cross-protocol conversion, reasoning replay constraints are preserved naturally between the client and upstream.

Credentials are delegated to the host: the plugin declares credential parsing, and the management page adds/removes keys through host callbacks. Key rotation, rate limiting, and token accounting are handled entirely by the host without duplicating key pools in the plugin.

Thinking parameters are passed through as written by the client according to its native protocol; the plugin does not modify them (see ADR-0004).

Upstream HTTP 401, 402, and 429 status codes pass through directly so the host scheduler can rotate credentials. If an upstream stream drops unexpectedly, the plugin emits a terminal error event conforming to the protocol rather than synthesizing a successful termination.

## User Stories

1. As a Claude Code user, I want to set `ANTHROPIC_BASE_URL` to CLIProxyAPI and use `deepseek-cpa/deepseek-v4-pro` without modifying client code.
2. As a Claude Code user, I want multi-turn thinking sessions with tool calls not to encounter HTTP 400 errors, so long tasks complete reliably.
3. As a Claude Code user, I want reasoning effort to take effect through familiar `output_config.effort` without learning DeepSeek-specific internal fields.
4. As an OpenAI SDK user, I want `reasoning_effort` and `thinking.type` to control thinking modes according to official documentation.
5. As a Responses API user, I want `reasoning.effort` to control thinking strength without changing client calling code.
6. As a Responses API user, I want streaming events to preserve original types and order so incremental UI rendering does not break.
7. As a Responses API user, I want to receive a `response.failed` event when an upstream stream fails, distinguishing failure from normal completion.
8. As an Anthropic user, I want to receive an `error` event when an upstream stream fails, allowing standard client error handling.
9. As a chat user, I want to receive an error payload before `[DONE]` on stream failure rather than mistaking truncation for success.
10. As an operator, I want all DeepSeek keys managed by the host credential system without plaintext keys in plugin configuration.
11. As an operator, I want to manage DeepSeek keys in the CLIProxyAPI web UI without modifying files on the server.
12. As an operator, I want the host to automatically switch to the next key when a key's quota is exhausted.
13. As an operator, I want upstream error codes passed through transparently to observe true 429 or 401 errors in host logs.
14. As an operator, I want plugin registration not to depend on network connectivity, ensuring proxy startup never fails if DeepSeek is unreachable.
15. As an operator, I want the model list fetched lazily when first needed, avoiding plugin updates solely for new model releases.
16. As an operator, I want to manually trigger model list refresh in the management UI to immediately detect new DeepSeek models.
17. As an operator, I want published model lists to match upstream availability, preventing deprecated models from lingering.
18. As an operator, I want to fall back to built-in default models if upstream is unreachable, ensuring valid model options exist during cold starts.
19. As an operator, I want clients to access this plugin strictly via the `deepseek-cpa/` prefix, preventing conflicts with built-in channels.
20. As an operator, I want bare `deepseek-*` model names to continue routing through standard host paths without behavior changes.
21. As an operator, I want model suffixes like `deepseek-cpa/deepseek-v4-pro[1m]` to forward properly to upstream for extended context windows.
22. As an operator, I want request usage recorded as structured logs for external analytics pipelines.
23. As an operator, I want only genuinely implemented capability surfaces declared, avoiding empty identity wrappers.
24. As a maintainer, I want protocol differences isolated in one place, making future protocol additions straightforward.
25. As a maintainer, I want upstream payload bytes to match original client bodies byte-for-byte (except model name), verified by automated tests.
26. As a maintainer, I want capability logic directly testable in pure Go without cgo or JSON envelopes for fast test execution.
27. As a maintainer, I want thin ABI smoke tests to catch broken cgo export signatures immediately.
28. As a maintainer, I want dependencies pinned to specific CLIProxyAPI versions, catching upstream contract changes at build time.
29. As a privacy-conscious user, I want no keys, `StorageJSON`, or raw bodies logged.
30. As a new user, I want clear diagnostics on registration or configuration errors.

## Implementation Decisions

**Plugin Identifier and Namespace:** The plugin ID is `deepseek-cpa`, matching the dynamic library filename, the `plugins.configs.deepseek-cpa` configuration key, and the model namespace prefix. Client model names must follow `deepseek-cpa/<upstream-model-name>`. Stripping the prefix yields the upstream model name, including suffixes like `[1m]`. Model names without the prefix are not processed by this plugin and fall back to the host's standard routing.

**Protocol Surface Alignment:** The plugin declares the `executor` capability with three input and output formats: `chat-completions`, `responses`, and `anthropic`. The outbound endpoint is determined solely by the inbound protocol surface without extra configuration (ADR-0001).

**No Identity Capability Wrappers:** `request_translator`, `response_translator`, `request_normalizer`, `response_normalizer`, `request_interceptor`, `response_interceptor`, `response_stream_interceptor`, `model_router`, and `command_line_plugin` are not declared. Only five functional surfaces are declared: `auth_provider`, `model_provider`, `executor`, `usage_plugin`, and `management_api`.

**Credential Ownership:** Implements `auth.identifier` and `auth.parse`. `auth.login.start`, `auth.login.poll`, and `auth.refresh` explicitly return unsupported because static DeepSeek keys never expire. Credentials are created via the management page calling host auth save callbacks (ADR-0002).

**Model Publication:** Published via `model.static` with `executor_model_scope: static`.
- Registration (`plugin.register`) performs zero network I/O and only validates configuration syntax.
- `model.static` lists models: returns built-in defaults if no credentials exist; probes upstream `/models` and caches results once a credential is present. Probe failures log a warning and fall back to defaults without blocking plugin startup.
- Upstream probing is authoritative: models not returned upstream are not registered and cannot be routed.
- The management API allows explicit re-probing; the host applies the new list on next plugin reload or restart.

**Thinking Parameters Passthrough:** `thinking_applier` is not implemented (ADR-0004 superseding ADR-0003). Host applier lookups use protocol format names (`claude`/`openai`/`codex`) rather than plugin IDs (`deepseek-cpa`), making plugin appliers unreachable. Furthermore, the host does not strip thinking fields on `UserDefined` models, allowing client thinking parameters to pass through directly.

**Error Semantics:** Upstream status codes (401, 402, 429) pass through directly without executor-level retries, enabling the host scheduler to rotate credentials.

**Streaming Execution:** Chunk framing depends on the protocol surface:
- `responses` and `anthropic`: The host forwards executor chunks verbatim, requiring complete SSE frames.
- `chat-completions`: The host wraps chunks in `data: <payload>` and appends `[DONE]`, requiring **bare JSON payloads** from the plugin. Handing pre-framed chunks causes double `data: data:` prefixes.

Streams ending without an upstream terminal event are treated as failures and emit appropriate protocol error events (`response.failed` for responses, `error` for anthropic, bare error JSON for chat).

**Usage Tracking:** Implements `usage_plugin` to log structured records. Does not store, aggregate, or serve dashboards.

**Management API:** Implements `management_api` providing key listing/addition/removal and explicit model list refreshes under authenticated `/v0/management` routes.

**Logging Hygiene:** Keys, `StorageJSON`, raw request bodies, and response bodies are strictly never logged.

## Testing Strategy

- **Seam A (Main Logic):** Pure Go tests directly invoking capability methods on `*Plugin`. Tests model prefix stripping, endpoint mapping, thinking passthrough, error code forwarding, stream failure framing, and usage logging.
- **Seam B (Transport Boundary):** Verifies byte-for-byte fidelity between client request bodies and upstream payloads (except model name) across all three protocol surfaces.
- **Seam C (ABI Smoke):** Python/ctypes smoke tests loading the built dynamic library, checking ABI versioning, symbol export, and error envelopes.

## Out of Scope

- Cross-protocol translation (handled by built-in host converters).
- Custom key pools and in-plugin credential rotation (delegated to host).
- Usage dashboards, aggregation, and billing.
- `model_router` implementation.
- Third-party relays or unofficial reverse proxies.
- Intercepting bare `deepseek-*` model requests.
