# Host-Managed Credentials Without Model Router

DeepSeek API keys are static keys that do not expire: there is no OAuth, no device code flow, and no token refresh interval. Implementing `auth.login.start`, `auth.login.poll`, or `auth.refresh` would just be no-op boilerplate to satisfy interface signatures.

We choose to implement `auth.identifier` and `auth.parse` on the `auth_provider` capability, and have the management page invoke `host.auth.save` via `/v0/management` routes to write credentials. The other three methods explicitly return unsupported. The benefit is that credentials, scheduling, and usage logging remain completely in the host's control; the plugin does not duplicate a key pool.

Consequently, **we do not implement `model_router`**: direct routing with `TargetKind: "self"` is documented as acting "without selecting an auth record", which bypasses the host's credential selection mechanism. Models are announced by `model_provider` via `model.static` with `executor_model_scope: static`, leaving routing to the host's standard `model → provider → credential` path.

Upstream HTTP 401, 402, and 429 status codes are passed through transparently without retrying inside the executor, allowing the host scheduler to observe them and failover to the next credential.
