# 协议对齐直通，不做跨协议翻译

DeepSeek 提供 `chat-completions`、`anthropic`、`responses` 三个协议面，且带 `tools` 的 thinking 会话要求历史 `reasoning_content` 原样回传，漏一轮即 400。任何跨协议翻译层都会在某个字段上丢信息，而这里的代价是会话直接不可用而不是降级。

因此插件声明三个协议面同时作为 `executor_input_formats` 和 `executor_output_formats`，把客户端的原始协议 body 送进同协议的上游端点，只在模型名上改写。代价是插件不支持「Anthropic 客户端打 `chat-completions` 端点」这类交叉组合——需要交叉的请求继续走宿主内置转换器，那本来就不是本插件要解决的问题。

由此推论：`request_translator`、`response_translator`、`request_normalizer`、`response_normalizer`、`request_interceptor`、`response_interceptor`、`response_stream_interceptor` 在本插件里全是恒等函数，一律不声明。声明了就等于给自己养一条永不执行的分支。
