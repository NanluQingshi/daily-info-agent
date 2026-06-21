# LLM 接入踩坑记录

**项目**：Daily Info Agent  
**接入平台**：中科大 LLM 平台（`api.llm.ustc.edu.cn`，OpenAI 兼容接口）  
**更新时间**：2026-06-19

---

## 背景

Agent 使用 OpenAI-compatible SDK（`go-openai`）对接 USTC LLM 平台，调用链路：

```
用户消息
  → POST /api/chat
    → ExtractTopic()      # 提取用户意图、关键词、分类
    → FetchForTopic()     # 抓取相关新闻
    → ProcessBatch()      # AI 批量摘要
    → 返回 ChatResponse
```

以下问题均在调试"返回 `failed to understand the request topic`"这一报错时排查出来。

---

## 一、API 兼容性问题

### 1.1 模型名称失效（404 Model Not Found）

**现象**

对话接口稳定返回 500，服务端日志：

```
deepseek unavailable: error, status code: 404, message: Model not found
```

**排查过程**

直接用 curl 打 USTC API，确认模型层面报错：

```bash
curl https://api.llm.ustc.edu.cn/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -d '{"model":"glm-5.2", "messages":[...]}'

# 返回
{"error": {"message": "litellm.NotFoundError: ...No fallback model group found for original model_group=glm-5.2"}}
```

**根本原因**

`.env` 里配的 `LLM_MODEL_ID=glm-5.2` 已从平台下线，但配置未跟进更新。

**修复**

```bash
# .env
LLM_MODEL_ID=deepseek-v4-flash-ascend
```

**经验**

- LLM 平台会不定期下线/重命名模型，**不能假设模型名永久有效**
- 上线前应通过 curl 直接验证模型可达性，而不只依赖代码测试
- `.env.example` 要标注「已验证可用」和「已知不可用」，避免后人踩坑

---

### 1.2 `response_format` 参数不兼容（4xx 请求被拒）

**现象**

即使模型名修正后，部分 API 调用仍然失败。

**根本原因**

代码中使用了 OpenAI 原生的 `response_format: json_object` 参数：

```go
ResponseFormat: &openai.ChatCompletionResponseFormat{
    Type: openai.ChatCompletionResponseFormatTypeJSONObject,
},
```

USTC 平台声称 OpenAI-compatible，但 **并不支持所有 OpenAI 扩展参数**。`response_format` 会被直接拒绝并返回 4xx。

**修复**

移除 `ResponseFormat` 字段，改用 Prompt 明确要求输出 JSON：

```go
// 移除 ResponseFormat，改为在 system prompt 里约束
Messages: []openai.ChatCompletionMessage{
    {
        Role:    openai.ChatMessageRoleSystem,
        Content: "You are a helpful assistant. Output ONLY valid JSON — no markdown, no explanation.",
    },
    ...
}
```

**经验**

- "OpenAI-compatible"只是协议层面兼容，不代表所有参数都支持
- 接入非 OpenAI 平台时，应优先用 curl 逐个验证参数，而不是照搬 OpenAI 文档
- 通过 Prompt 约束输出格式比依赖 API 参数更通用

---

## 二、响应解析问题

### 2.1 模型输出 Markdown 代码块（JSON 解析失败）

**现象**

即使 Prompt 明确写了"Output ONLY valid JSON"，部分模型仍然返回：

````
```json
{"category":"科技/AI","keywords":["AI"],"summary":"..."}
```
````

直接 `json.Unmarshal` 会报错，因为内容不是合法 JSON 字符串。

**修复**

新增 `extractJSON()` 函数，在解析前自动剥离 markdown 代码块和前后多余文字：

```go
func extractJSON(raw string) string {
    raw = strings.TrimSpace(raw)

    // 剥离 ```json ... ``` 或 ``` ... ```
    if strings.HasPrefix(raw, "```") {
        if idx := strings.Index(raw, "\n"); idx != -1 {
            raw = raw[idx+1:]
        }
        if idx := strings.LastIndex(raw, "```"); idx != -1 {
            raw = strings.TrimSpace(raw[:idx])
        }
    }

    // 定位第一个 JSON 容器字符
    if start := strings.IndexAny(raw, "{["); start > 0 {
        raw = raw[start:]
    }
    // 裁掉末尾多余内容
    if end := strings.LastIndexAny(raw, "]}"); end != -1 && end < len(raw)-1 {
        raw = raw[:end+1]
    }

    return strings.TrimSpace(raw)
}
```

**经验**

- 不能完全信任模型会严格遵守输出格式指令，**防御性解析是必要的**
- 解析层应能处理：纯 JSON、代码块包裹的 JSON、前有说明文字的 JSON
- 此函数应作为所有 LLM JSON 响应的统一入口

---

## 三、健壮性问题

### 3.1 关键路径无重试（首次失败直接报错）

**现象**

`ExtractTopic` 遇到网络抖动或模型短暂过载时，直接返回错误，用户看到"failed to understand the request topic"。

**根本原因**

`ProcessBatch`（批处理）有重试逻辑（最多 2 次，间隔 2s），但 `ExtractTopic`（单次 topic 提取）没有，是单次调用。

**修复**

为 `ExtractTopic` 加入与 `ProcessBatch` 一致的重试机制：

```go
var lastErr error
for attempt := 0; attempt < 2; attempt++ {
    if attempt > 0 {
        select {
        case <-ctx.Done():
            return TopicResult{}, ctx.Err()
        case <-time.After(deepSeekRetryWait): // 2s
        }
    }

    resp, err := p.client.CreateChatCompletion(...)
    if err != nil {
        lastErr = err
        continue      // 重试
    }
    // 解析成功则直接返回
    ...
}
return TopicResult{}, &LLMUnavailableError{Cause: lastErr}
```

注意：**Parse 错误不重试**（同一模型大概率返回相同格式，重试无意义）。

**经验**

- 所有对外 API 调用都应有重试，尤其是 LLM 这种推理耗时长、偶发超时的服务
- 重试策略：网络/服务端错误重试，解析/业务逻辑错误不重试
- 各调用路径的重试逻辑要对齐，避免"批处理有保障、单次调用裸奔"

---

## 排查方法论

遇到"对话不可用"类问题，建议按以下顺序排查：

```
1. curl 直接打 LLM API
   → 确认网络通、Key 有效、模型存在

2. 检查请求参数
   → 逐个字段验证平台是否支持（response_format 等扩展参数是重灾区）

3. 检查响应解析
   → 手动打印原始 raw 响应，看是否有 markdown 包装

4. 检查错误传播路径
   → 从 handler 错误信息反推，找到真正 fail 的那一层
```

---

## 可用模型速查（2026-06）

| 模型 | 状态 | 适合场景 |
|---|---|---|
| `deepseek-v4-flash-ascend` | ✅ 可用 | 推荐，速度快，指令遵循好 |
| `qwen-chat` | ✅ 可用 | 备选 |
| `deepseek-v4-pro` | ✅ 可用 | 效果更强，速度较慢 |
| `qwen3.6-chat` | ⚠️ 思考模型 | `content` 字段为 null，需读 `reasoning_content` |
| `glm-5.2` | ❌ 404 下线 | 勿用 |
| `glm-chat` | ❌ 连接错误 | 勿用 |
