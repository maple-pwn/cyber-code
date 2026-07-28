# Providers

## Anthropic

`provider: anthropic` 使用 Messages API，支持流式文本、思考块、工具调用和 token counting。

```yaml
provider: anthropic
base_url: https://api.anthropic.com
model: claude-sonnet-4-20250514
api_key_env: ANTHROPIC_API_KEY
```

## OpenAI-compatible

`provider: openai`、`openai-compatible` 或 `deepseek` 使用 `/v1/chat/completions`。自定义 Base URL 保留反向代理路径前缀，支持流式文本、usage 和并行工具调用增量。

```yaml
provider: openai-compatible
base_url: https://api.deepseek.com
model: deepseek-v4-pro
api_key_env: DEEPSEEK_API_KEY
```

后端必须实现 SSE `[DONE]` 终止语义。若后端不支持工具调用，应在构造 Provider 时关闭相应能力；编码 Agent 主链路需要工具调用支持。

Bedrock、Vertex 和 Azure 适配器具有受管认证与契约测试，但真实云验收需要用户控制的账号、区域和权限，默认测试不会访问云服务。
