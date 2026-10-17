# deepseek-cpa

CLIProxyAPI 的 DeepSeek 原生插件。客户端按自己习惯的协议访问，插件把请求送进 DeepSeek 同协议端点，不做跨协议翻译。

## Language

**协议面**：
DeepSeek 对外提供的三种入站协议之一：`chat-completions`、`anthropic`、`responses`。一个插件实例同时服务三个协议面。
_Avoid_: 格式、端点、API 类型

**协议对齐直通**：
客户端用哪个协议面发请求，插件就把请求原样送进 DeepSeek 的同一个协议面。插件不在协议面之间转换，只在必要处改写模型名。
_Avoid_: 翻译、适配、转换层

**上游端点**：
承载某个协议面的 DeepSeek 服务地址。`chat-completions` 与 `responses` 在 `https://api.deepseek.com`，`anthropic` 在 `https://api.deepseek.com/anthropic`。
_Avoid_: 后端、通道

**reasoning 回放**：
`chat-completions` 协议面上，带 `tools` 的 thinking 会话要求历史所有轮的 `reasoning_content` 原样回传，漏一轮就 400；不带 `tools` 时上游会忽略它。这是协议对齐直通要保护的首要事实。实测 `anthropic` 协议面不施加这条约束，所以「没有 400」不能当作保真的证据，保真只能由字节比对证明。
_Avoid_: 上下文恢复、思维链传递

**凭证**：
宿主管理的一条 DeepSeek API key 记录。`StorageJSON` 由插件拥有，插件不自己维护密钥池。
_Avoid_: 密钥、key、账号

**模型命名空间**：
客户端可见的 `deepseek-cpa/` 前缀。前缀之后的部分原样作为上游模型名，包括 `[1m]` 这类后缀。
_Avoid_: 别名、模型组

**能力面**：
插件向宿主声明的可调用接口，例如 `executor`、`auth_provider`。声明了就必须真的干活。
_Avoid_: 能力、模块、功能

**懒探测**：
首次需要模型列表时才向上游取并缓存。注册阶段不联网，因此上游不可达不影响插件加载。
_Avoid_: 预加载、启动拉取
