# Do Not Implement thinking_applier; Pass Thinking Fields As-Is

ADR-0003 originally decided to implement `thinking_applier` by sniffing payload shapes. During implementation, host source code inspection revealed that this capability surface is unreachable for this plugin, so we decided not to implement it.

The host looks up appliers using the protocol format name on the request path (`internal/thinking/apply.go`): responses maps to `codex`, anthropic to `claude`, and chat to `openai`, calling `GetProviderApplier(<format_name>)`. However, plugin appliers are registered using the plugin's `thinking.identifier` (`internal/pluginhost/adapters_usage_translation.go`), which for this plugin is `deepseek-cpa`. Because these names never match, `thinking.apply` on the plugin would never be called.

Registering under native format names is also disallowed: `RegisterPluginProvider` immediately rejects registrations that collide with built-in appliers (`claude`, `openai`, `codex`). Even if allowed, hijacking native names would affect all requests across the entire process, not just requests for this plugin's models.

Furthermore, the host does not strip thinking fields from plugin models: this plugin announces models with `UserDefined: true`, so the host takes the passthrough path, allowing client-specified thinking parameters to reach upstream directly. Live testing on the `anthropic` surface confirmed thinking sessions work correctly via this exact path.

Conclusion: We do not declare `thinking_applier`, reducing declared capability surfaces from six to five. Client thinking fields pass through untouched; the plugin's responsibility is not to modify them. If the host supports looking up appliers by provider key in the future, this capability can be revisited based on ADR-0003.
