# 不实现 thinking_applier，thinking 字段原样透传

ADR-0003 决定实现 `thinking_applier`，靠载荷形状分发三套 thinking 字段。实施时查宿主源码发现该能力面对本插件不可达，因此改为不实现。

宿主在请求路径上用协议格式名查找 applier（`internal/thinking/apply.go`）：responses 被规整为 `codex`，anthropic 为 `claude`，chat 为 `openai`，然后调用 `GetProviderApplier(<格式名>)`。而插件的 applier 由宿主按插件返回的 `thinking.identifier` 注册（`internal/pluginhost/adapters_usage_translation.go`），本插件返回的是 `deepseek-cpa`。两个名字永不相等，所以插件实现的 `thinking.apply` 永远不会被调用。

改用原生格式名注册同样不可行：`RegisterPluginProvider` 在发现同名原生 applier 时直接拒绝注册，而 `claude`、`openai`、`codex` 都是原生名。即便能注册，劫持这些名字也会影响同进程内所有走该格式的请求，而不只是本插件的模型。

同时宿主不会剥离插件模型的 thinking 字段：本插件的模型以 `UserDefined: true` 公布，宿主对这类模型走 passthrough 分支，客户端写入的 thinking 参数原样到达上游。真机实测 anthropic 协议面的 thinking 会话工作正常，正是走这条路径。

结论：不声明 `thinking_applier`，能力面从六个减为五个。客户端的 thinking 字段原样透传，插件的职责是不要碰它们。将来宿主支持按 provider key 查找 applier 时，可以重新提出这个能力面，届时的实现依据见 ADR-0003。
