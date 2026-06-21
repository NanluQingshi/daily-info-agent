# LLM 接入踩坑记录

**项目**：Daily Info Agent  
**接入平台**：中科大 LLM 平台（`api.llm.ustc.edu.cn`，OpenAI 兼容接口）  
**更新时间**：2026-06-20

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

## 四、Agent 实现阶段问题

> 以下问题在将 chat 模式改造为 LLM Agent（Tool Calling）时遇到。

### 4.1 go-openai SDK 丢弃 `reasoning_content` 字段

**现象**

测试用例 `TestRunner_ReasoningContent_UsedWhenContentEmpty` 失败：

```
expected: "思考过程中得出的答案"
actual  : "抱歉，我暂时无法生成回复，请稍后再试。"
```

**根本原因**

原实现通过 SDK 发起请求，再将 SDK 响应 struct re-marshal 成 JSON 以便自定义解析：

```go
resp, err := r.client.CreateChatCompletion(ctx, req)  // SDK 解析
raw, _    := json.Marshal(resp)                        // 再序列化
decodeResponse(raw)                                    // 读 reasoning_content
```

go-openai 的 `ChatCompletionResponse` struct 没有 `reasoning_content` 字段，SDK 解析时该字段被静默丢弃，re-marshal 后自然取不到。

**修复**

绕开 SDK 的响应解析，改用原生 `net/http` 直接获取原始 JSON body：

```go
// agent.go — callLLM
httpResp, _ := r.httpClient.Do(httpReq)
raw, _      := io.ReadAll(httpResp.Body)
json.Unmarshal(raw, &resp)   // 自定义 struct，含 reasoning_content
```

同时将 `*openai.Client` 从 Runner 中移除，改为存储 `baseURL` 和 `apiKey`，Runner 自己管理 HTTP 客户端。

**经验**

- 使用第三方 SDK 时，**SDK struct 只保留它"认识"的字段**，平台扩展字段会丢失
- 需要读取非标准字段时，应在 HTTP 层拦截原始响应，而不是依赖 SDK 的类型映射
- 这类问题通常在测试阶段才暴露（单元测试 mock 不会丢字段，真实调用才丢）

---

### 4.2 Echo 30s 超时中间件截断 Agent 请求

**现象**

新闻查询请求返回：

```html
<html><head><title>Timeout</title></head><body><h1>Timeout</h1></body></html>
```

前端解析 JSON 报错，curl 返回空响应。

**根本原因**

`cmd/agent/main.go` 注册了全局 30s 超时中间件：

```go
e.Use(middleware.TimeoutWithConfig(middleware.TimeoutConfig{
    Timeout: 30 * time.Second,
}))
```

Agent 模式下，一次新闻查询包含：
- 第一次 LLM 调用（决定调工具）≈ 5–15s
- RSS / NewsAPI 抓取 ≈ 5–10s  
- 第二次 LLM 调用（生成回复）≈ 5–15s

三步串行，合计轻松超过 30s，中间件直接截断并返回 HTML 错误页。

**修复**

将 `/api/chat` 加入超时跳过列表：

```go
Skipper: func(c echo.Context) bool {
    return c.Path() == "/api/chat" ||
           strings.HasSuffix(c.Path(), "/stream")
},
```

**经验**

- 全局超时中间件适合普通 CRUD 接口，**不适合 Agent / AI 类耗时请求**
- 引入 Tool Calling 后，响应时间从单次 LLM 延迟变为「多次 LLM + 网络 IO」的累加，要重新评估超时阈值
- 排查此类问题的关键：直接看 curl 的原始响应体，HTML 响应 = 中间件/代理截断，JSON 才是业务层的错误

---

### 4.3 HTTP 502（前端代理找不到后端）

**现象**

前端页面访问 `http://localhost:5173/api/chat` 返回 502。

**根本原因**

5173 是 Vite 开发服务器端口，Vite 配置了代理：

```ts
// vite.config.ts
proxy: { "/api": "http://localhost:8080" }
```

502 意味着 Vite 能接收请求，但转发到 8080 时连接被拒——**后端进程没有运行**。

这不是代码 bug，是进程启动顺序问题。

**修复**

确保后端先于前端启动。在 Makefile 中新增 `dev` target，同时管理两个进程：

```makefile
dev: build
	@./$(BINARY) --mode=server & \
	  BACKEND_PID=$$!; \
	  cd web && npm run dev; \
	  kill $$BACKEND_PID 2>/dev/null || true
```

运行 `make dev` 即可，Ctrl+C 退出时两个进程一起终止。

**经验**

- 前后端分离项目的 502 优先检查「后端是否在跑」，而不是查代码
- `lsof -i :8080 | grep LISTEN` 可以快速确认端口占用状态
- 开发环境依赖多个进程时，应通过 Makefile / 脚本统一管理启动，避免手动遗漏

---

## 排查方法论

遇到"对话不可用"类问题，建议按以下顺序排查：

```
1. 确认后端在跑
   → lsof -i :8080 | grep LISTEN
   → 502 = 后端没起；500 = 后端有错

2. curl 直接打 LLM API（绕开业务代码）
   → 确认网络通、Key 有效、模型存在
   → 逐个字段验证平台是否支持（response_format 等扩展参数是重灾区）

3. 看原始响应体
   → HTML 响应 = 中间件/代理截断（超时、nginx 等）
   → 空响应 = 超时或连接中断
   → JSON 才是业务层错误，再往下查

4. 检查响应解析
   → 打印原始 raw 字符串，看是否有 markdown 代码块包装
   → 检查 SDK 是否丢弃了平台扩展字段（reasoning_content 等）

5. 检查错误传播路径
   → 从 handler 错误信息反推，找到真正 fail 的那一层
   → 注意区分：解析错误不重试；网络/5xx 错误才重试
```

---

## 可用模型速查（2026-06）

| 模型 | 状态 | 备注 |
|---|---|---|
| `deepseek-v4-pro` | ✅ 当前使用 | 支持 Tool Calling，推理质量高，适合 Agent 模式 |
| `deepseek-v4-flash-ascend` | ✅ 可用 | 速度快，适合简单摘要任务 |
| `qwen-chat` | ✅ 可用 | 备选 |
| `qwen3.6-chat` | ⚠️ 思考模型 | `content` 字段为空，答案在 `reasoning_content` 里；需绕过 SDK 读原始 JSON |
| `glm-5.2` | ❌ 404 下线 | 勿用（Key 授权列表里有但实际路由 404） |
| `glm-chat` | ❌ 连接错误 | 勿用 |

> **注意**：`qwen3.6-chat` 等思考模型的 `content` 字段在思考阶段为空，如果用 go-openai SDK 的类型映射会拿到空字符串，需参照 4.1 节用原始 HTTP 读取 `reasoning_content`。
