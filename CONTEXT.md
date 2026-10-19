# deepseek-cpa

Native DeepSeek plugin for CLIProxyAPI. Clients connect using their native protocol; the plugin forwards requests directly into DeepSeek's matching protocol endpoint without cross-protocol translation.

## Domain Vocabulary

**Protocol Surface**:
One of the three inbound protocols DeepSeek serves: `chat-completions`, `anthropic`, and `responses`. A single plugin instance serves all three protocol surfaces simultaneously.
_Avoid_: format, endpoint, API type.

**Protocol-Aligned Passthrough**:
Whichever protocol surface the client uses, the plugin forwards the payload verbatim to DeepSeek's identical protocol surface. The plugin does not translate between protocol surfaces; it only rewrites the model name prefix where necessary.
_Avoid_: translation, adaptation, conversion layer.

**Upstream Endpoint**:
The DeepSeek service URL hosting a specific protocol surface. `chat-completions` and `responses` reside at `https://api.deepseek.com`; `anthropic` resides at `https://api.deepseek.com/anthropic`.
_Avoid_: backend, channel.

**Reasoning Replay**:
On the `chat-completions` surface, a thinking session with `tools` requires all historical turns of `reasoning_content` to be echoed back verbatim. Omitting even a single turn results in an immediate HTTP 400 error. Without `tools`, upstream ignores missing reasoning. This is the primary constraint protected by protocol-aligned passthrough. Tested evidence shows the `anthropic` surface does not enforce this strict rule, so "no HTTP 400" is not proof of fidelity—fidelity is only proven by byte-for-byte comparison.
_Avoid_: context recovery, chain-of-thought transfer.

**Credential**:
A DeepSeek API key entry managed by the host. `StorageJSON` is owned by the plugin; the plugin does not maintain its own key pool.
_Avoid_: secret, key, account.

**Model Namespace**:
The client-visible `deepseek-cpa/` prefix. Everything after the prefix is forwarded verbatim as the upstream model name, including suffixes such as `[1m]`.
_Avoid_: alias, model group.

**Capability Surface**:
An interface declared by the plugin to the host (e.g., `executor`, `auth_provider`). If declared, it must perform actual work.
_Avoid_: capability, module, feature.

**Lazy Probing**:
Fetching and caching the model list from upstream only when first requested. The registration phase does no network I/O, ensuring upstream unavailability never blocks plugin loading.
_Avoid_: preloading, startup pulling.
