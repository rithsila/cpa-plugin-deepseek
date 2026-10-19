# cpa-plugin-deepseek

Native DeepSeek plugin for **CLIProxyAPI**. Clients connect using their native protocol, and the plugin forwards requests directly to DeepSeek's matching endpoint—**without cross-protocol translation**.

Plugin Identifier: `deepseek-cpa` (used as plugin ID, `.so`/`.dylib`/`.dll` filename, config key, and model namespace prefix).

## What It Solves

CLIProxyAPI's built-in `openai-compatibility` channel routes requests through a generic conversion pipeline: `client protocol → internal format → upstream protocol`.

DeepSeek has three protocol surfaces (`anthropic`, `chat-completions`, and `responses`). Each has different reasoning/thinking fields and event streaming rules. Any middleware that drops or shifts fields can break sessions:
- For DeepSeek thinking models with `tools`, all past `reasoning_content` must be echoed back byte-for-byte. Missing even one turn causes an immediate HTTP 400 error.
- Clients like Claude Code bind tool calls and thinking blocks tightly; cross-protocol conversion risks field mismatch.

This plugin only does three things:
1. Detect client protocol surface.
2. Select the matching DeepSeek endpoint.
3. Rewrite the model name prefix.

All other request and response bytes pass through untouched. See [ADR-0001](docs/adr/0001-protocol-aligned-passthrough.md).

| Client Protocol | DeepSeek Upstream Endpoint |
|---|---|
| `anthropic` (Claude Code, etc.) | `https://api.deepseek.com/anthropic/v1/messages` |
| `chat-completions` (OpenAI SDK, etc.) | `https://api.deepseek.com/chat/completions` |
| `responses` | `https://api.deepseek.com/responses` |

## Requirements

- **CLIProxyAPI** v8.0.32 (pinned in `go.mod`), built with cgo plugin support (`X-CPA-SUPPORT-PLUGIN: 1`).
- **Platforms**: `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64}`, `freebsd/amd64`.
  Matches the official CLIProxyAPI plugin-capable releases. (`freebsd/arm64` is excluded because upstream only provides a no-plugin build).

## Installation

Download the zip package for your platform from [Releases](https://github.com/rithsila/cpa-plugin-deepseek/releases) and extract it to the `plugins/` directory of CLIProxyAPI:

```bash
# Example for linux/amd64 (change platform as needed)
version=0.4.4
curl -sL -O "https://github.com/rithsila/cpa-plugin-deepseek/releases/download/v${version}/deepseek-cpa_${version}_linux_amd64.zip"
unzip -o "deepseek-cpa_${version}_linux_amd64.zip" -d <cliproxyapi>/plugins/
```

After extraction, the file must be named `deepseek-cpa.so` (macOS: `deepseek-cpa.dylib`, Windows: `deepseek-cpa.dll`). The plugin ID is taken directly from the filename.

Enable the plugin in `config.yaml`:

```yaml
plugins:
  enabled: true
  configs:
    deepseek-cpa:
      enabled: true
```

The plugin **requires no custom config fields**: endpoints are fixed to official DeepSeek URLs, and models are probed dynamically from upstream.

Verify plugin registration:

```bash
curl -s -H "X-Management-Key: <your-management-key>" \
  http://127.0.0.1:8317/v0/management/plugins | jq '.plugins[] | {id, registered, effective_enabled}'
```

## Managing DeepSeek Keys

API credentials are managed by the host proxy. You can manage them in two ways:

### 1. Web Management Page (Recommended)

Open: Management Panel → Plugins → **DeepSeek Keys** (`/v0/resource/plugins/deepseek-cpa/credentials`).

If you checked "Remember Password" during management panel login, the page automatically retrieves your management key. Otherwise, enter your management key once. You can then view, add, and remove DeepSeek API keys directly.

### 2. Command Line (Management API)

All endpoints require the `X-Management-Key` header:

```bash
# Add a key (adding the same key again updates the credential idempotently)
curl -s -X POST -H "X-Management-Key: <key>" -H "Content-Type: application/json" \
  -d '{"api_key":"sk-..."}' \
  http://127.0.0.1:8317/v0/management/plugins/deepseek-cpa/keys

# List keys (keys themselves are never returned)
curl -s -H "X-Management-Key: <key>" \
  http://127.0.0.1:8317/v0/management/plugins/deepseek-cpa/keys

# Delete a key
curl -s -X DELETE -H "X-Management-Key: <key>" \
  "http://127.0.0.1:8317/v0/management/plugins/deepseek-cpa/keys?name=deepseek-cpa-<hash>.json"
```

Credentials are saved in the host's auth directory (`oauth.auth-dir`) as `{"type":"deepseek-cpa","api_key":"..."}`.

## Using Models

Clients must use the `deepseek-cpa/` prefix. Everything after the prefix is forwarded as the upstream model name:

```python
# OpenAI SDK
from openai import OpenAI

client = OpenAI(base_url="http://127.0.0.1:8317/v1", api_key="<proxy-client-key>")
response = client.chat.completions.create(
    model="deepseek-cpa/deepseek-v4-pro",
    messages=[{"role": "user", "content": "Hello!"}],
)
```

```bash
# Claude Code
export ANTHROPIC_BASE_URL=http://127.0.0.1:8317
export ANTHROPIC_AUTH_TOKEN=<proxy-client-key>
export ANTHROPIC_MODEL=deepseek-cpa/deepseek-v4-pro
```

### Upstream Model Discovery

The plugin probes `https://api.deepseek.com/models` and publishes the active model list:
- If no DeepSeek credential exists yet: returns a built-in bootstrap list without making network requests.
- If upstream probing fails: falls back to the built-in list and logs a message.
- To refresh the model list immediately: `POST /v0/management/plugins/deepseek-cpa/models/refresh`. The host loads the updated list upon reload or restart.

## Usage Logs

Every completed request emits a structured JSON log line:

```
request usage {"model":"deepseek-cpa/deepseek-v4-pro","response_model":"deepseek-v4-pro",
"provider":"deepseek-cpa","stream":true,"failed":false,"status":200,
"input_tokens":84,"output_tokens":8,"reasoning_tokens":8,"cached_tokens":0,"total_tokens":92,
"latency_ms":1187,"ttft_ms":1134,"request_id":"…","trace_id":"…"}
```

No API keys, credentials, prompt contents, or response bodies are ever logged.

## Known Limitations

- **Official DeepSeek endpoints only**: Third-party relays or reverse proxies are not supported.
- **Model names follow upstream**: Models not returned by `https://api.deepseek.com/models` cannot be routed.
- **No `thinking_applier`**: Plugin does not modify thinking parameters; client thinking configurations pass through as-is (see [ADR-0004](docs/adr/0004-no-thinking-applier.md)).
- **Usage logs lack protocol format**: The host's usage event format does not report the incoming protocol surface.

## Development

```bash
./build.sh                                  # Build .so/.dylib for current platform into dist/
go test ./...                               # Run unit tests (offline)
python3 test/abi_smoke.py dist/deepseek-cpa.darwin.arm64  # Smoke test dynamic library ABI
./scripts/package-release.sh 0.4.4          # Package release archive + checksums
python3 test/package_check.py               # Validate packages against registry.json
```

### E2E Tests

End-to-end tests require a running CLIProxyAPI host with a valid DeepSeek key:

```bash
python3 test/anthropic_e2e.py   # Multi-turn thinking + tool call session
python3 test/stream_e2e.py      # Terminal event validation for anthropic & responses
python3 test/chat_e2e.py        # Thinking on/off comparison, usage tokens, [DONE]
```

### Releasing

Releases are triggered by pushing a git tag:

```bash
# 1. Update version in registry.json to match tag
# 2. Tag and push
git tag v0.4.4 && git push origin v0.4.4
```

CI builds all targets across the matrix in [platforms.txt](platforms.txt) and creates a GitHub Release with SHA-256 checksums.

## Documentation

- [CONTEXT.md](CONTEXT.md) — Domain glossary and terms
- [docs/adr/](docs/adr/) — Architecture Decision Records
- [docs/spec/0001-deepseek-cpa-plugin.md](docs/spec/0001-deepseek-cpa-plugin.md) — Full technical specification
