# Protocol-Aligned Passthrough Without Cross-Protocol Translation

DeepSeek provides three protocol surfaces: `chat-completions`, `anthropic`, and `responses`. For thinking models making tool calls, all past `reasoning_content` turns must be echoed back verbatim; omitting even one turn causes an immediate HTTP 400 error. Any cross-protocol translation layer risks dropping or altering fields, making multi-turn sessions fail completely rather than gracefully degrading.

Therefore, the plugin declares all three protocol surfaces simultaneously as `executor_input_formats` and `executor_output_formats`. It forwards the client's original protocol body directly to the matching upstream endpoint, rewriting only the model name. The trade-off is that the plugin does not support mixed cross-combinations (such as an Anthropic client calling a `chat-completions` endpoint)—requests requiring protocol conversion continue to use the host's built-in translators, which is outside this plugin's scope.

Consequence: `request_translator`, `response_translator`, `request_normalizer`, `response_normalizer`, `request_interceptor`, `response_interceptor`, and `response_stream_interceptor` are all identity functions in this plugin and are explicitly not declared. Declaring them would only create dead branches that are never executed.
