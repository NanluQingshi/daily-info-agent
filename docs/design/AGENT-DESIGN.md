# LLM Agent 设计文档

**版本**：1.0  
**日期**：2026-06-19  
**状态**：待实现  
**依赖文档**：[DESIGN.md](DESIGN.md)

---

## 一、背景与目标

### 现状

当前 chat 功能采用**硬编码六步流水线**：

```
ExtractTopic → FetchForTopic → ProcessBatch → Verify → Sort → Summary
```

LLM 只作为 NLP 工具（提取关键词、生成摘要），所有流程控制由代码写死。  
导致的问题：
- 所有用户消息都当作新闻查询处理，无法回答非新闻类问题
- 无多轮对话记忆，每次请求独立
- 延迟高（串行调用，约 25s）

### 目标

将 chat 模式改造为**真正的 LLM Agent**：

- LLM 作为决策大脑，自主判断是否需要调工具
- 支持多轮对话（会话内记忆）
- 非新闻类问题直接回答，不走检索流水线

---

## 二、架构设计

### 2.1 整体架构

```
用户
 │  POST /api/chat  { message, session_id? }
 ▼
chat.Handler
 │
 ▼
agent.Runner  ←────────────────────────────────────────┐
 │                                                       │
 │  1. 构建 messages（系统提示 + 历史 + 当前消息）          │
 │                                                       │
 ▼                                                       │
LLM（deepseek-v4-pro）                                   │
 │                                                       │
 ├─ finish_reason: tool_calls ──► tool.Executor          │
 │                                    │ 执行工具          │
 │                                    │ 结果追加到 messages│
 │                                    └───────────────────┘
 │
 └─ finish_reason: stop ──► 返回最终回复给用户
```

### 2.2 Agent 循环（ReAct 模式）

```
loop:
  response = LLM(messages, tools)
  
  if response.finish_reason == "tool_calls":
    for each tool_call in response.tool_calls:
      result = execute(tool_call)
      messages.append(tool_result)
    goto loop
    
  if response.finish_reason == "stop":
    return response.content
```

最大迭代次数：**5 次**（防止无限循环）

### 2.3 工具定义

#### `search_news`

```json
{
  "name": "search_news",
  "description": "搜索最新新闻文章。当用户询问时事、行业动态、具体事件等需要实时信息的问题时调用。",
  "parameters": {
    "type": "object",
    "properties": {
      "keywords": {
        "type": "array",
        "items": { "type": "string" },
        "description": "3-5 个英文搜索关键词"
      },
      "category": {
        "type": "string",
        "enum": ["金融", "政治", "经济", "科技/AI", "国际"],
        "description": "新闻分类"
      }
    },
    "required": ["keywords", "category"]
  }
}
```

执行逻辑：调用 `fetcher.Manager.FetchForTopic(keywords, maxItems)`，返回文章列表（标题 + 摘要 + URL）。

> 后续可扩展工具：`get_article_detail`、`get_stats` 等。

### 2.4 会话记忆

- 每个会话由 **session_id**（UUID）标识，客户端首次请求时由服务端生成并返回
- 服务端维护内存中的 `map[sessionID][]Message`
- 每次请求追加新消息，保留最近 **20 条**（防止 context 过长）
- 服务重启后历史清空（当前阶段不持久化）

```
ChatRequest  { message: string, session_id?: string }
ChatResponse { ..., session_id: string }
```

### 2.5 系统提示（System Prompt）

```
你是 Daily Info Agent，一个专注于新闻资讯的 AI 助手。

你的能力：
- 通过 search_news 工具搜索实时新闻（金融、政治、经济、科技/AI、国际）
- 对搜索结果进行分析、总结和解读
- 回答用户关于新闻内容的追问

行为准则：
- 需要实时信息时，主动调用 search_news，不要凭记忆回答时事
- 闲聊、问候、询问你是谁等非新闻问题，直接回答，不要调工具
- 搜索结果为空时，如实告知，不要编造新闻
- 回复使用中文，简洁清晰
```

---

## 三、接口变更

### 3.1 Request（新增 session_id 字段）

```json
// 首次请求（不带 session_id）
{ "message": "最近 AI 有什么新进展？" }

// 后续多轮（带 session_id）
{ "message": "刚才说的 OpenAI 消息，能详细说说吗？", "session_id": "abc-123" }
```

### 3.2 Response（新增 session_id、reply 字段）

```json
{
  "session_id":     "abc-123",
  "reply":          "根据最新消息，OpenAI 宣布...",   // LLM 直接生成的回复
  "sources":        [...],                            // 调用工具时的来源文章
  "tool_called":    true,                             // 是否调用了工具
  "fetched_at":     "2026-06-19T12:00:00Z",
  "latency_ms":     3200
}
```

> 原 `extracted_topic`、`category`、`summary` 字段废弃，由 `reply` 替代。

---

## 四、包结构

```
internal/
├── agent/              # 新增
│   ├── agent.go        # Runner：Agent 循环主逻辑
│   ├── tools.go        # 工具注册与执行
│   ├── session.go      # 会话历史管理
│   └── prompt.go       # System prompt 常量
├── chat/
│   ├── handler.go      # 保留，改为调用 agent.Runner
│   └── handler_test.go
```

---

## 五、实现步骤

| 步骤 | 内容 | 关键文件 |
|---|---|---|
| 1 | 新建 `agent` 包，实现 Agent 循环 | `internal/agent/agent.go` |
| 2 | 实现工具层（`search_news` 执行逻辑） | `internal/agent/tools.go` |
| 3 | 实现会话管理（内存 map + TTL 清理） | `internal/agent/session.go` |
| 4 | 更新 `models.ChatRequest/ChatResponse` | `pkg/models/models.go` |
| 5 | 重写 `chat.Handler`，调用 `agent.Runner` | `internal/chat/handler.go` |
| 6 | 更新前端 API 类型和 session_id 传递 | `web/src/types/index.ts` 等 |
| 7 | 补测试 | `internal/agent/*_test.go` |

---

## 六、关键技术细节

### 6.1 reasoning_content 处理

`deepseek-v4-pro` 是思考模型，响应中包含 `reasoning_content` 字段。  
go-openai SDK 不原生解析此字段，需要手动从原始响应中提取：

```go
// 方案：使用 RawMessage，发起 HTTP 调用后手动解析 choices[0].message
type rawMessage struct {
    Role             string      `json:"role"`
    Content          string      `json:"content"`
    ReasoningContent string      `json:"reasoning_content"`
    ToolCalls        []ToolCall  `json:"tool_calls"`
}
```

### 6.2 Tool Call 消息格式

```
// LLM 返回 tool_calls 后，messages 追加两条：
1. assistant message（含 tool_calls）
2. tool message（含 tool_call_id 和执行结果）
```

### 6.3 并发安全

session map 需要加读写锁（`sync.RWMutex`）。

---

## 七、不在本次范围内

- 会话持久化到数据库
- 工具扩展（`get_article_detail` 等）
- 流式响应（SSE streaming）
- 用量统计与限速
