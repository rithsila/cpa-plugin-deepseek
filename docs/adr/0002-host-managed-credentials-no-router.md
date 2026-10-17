# 凭证交宿主管，且不实现 model_router

DeepSeek 的 API key 是不过期的静态密钥，没有 OAuth、没有 device code、没有 token 刷新周期。`auth.login.start` / `auth.login.poll` / `auth.refresh` 对它都是空活，实现它们只是为了勾满接口。

选择实现 `auth_provider` 的 `auth.identifier` + `auth.parse`，并让管理页通过 `/v0/management` 路由调用 `host.auth.save` 写入凭证，其余三个方法显式返回不支持。收益是凭证、调度、用量记账全在宿主手里，插件不复制一份密钥池。

由此**不实现 `model_router`**：`TargetKind: "self"` 的直连路由按文档明确「without selecting an auth record」，会绕过刚接上的宿主凭证选择，两者互斥。模型由 `model_provider` 以 `model.static` 公布、`executor_model_scope: static`，路由交给宿主正常的模型→提供商→凭证路径。

上游的 401 / 402 / 429 原样透传，不在 executor 内部重试——宿主调度器要看见这些状态码才能切下一个凭证。
