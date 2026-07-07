# 项目下一步规划 — Next Steps

**版本**: 1.0  
**日期**: 2026-07-03  
**分支**: `docs/next-steps-planning`  
**作者**: AtomCode

---

## 1. 项目现状总览

### 1.1 已完成的核心能力

| 模块 | 状态 | 说明 |
|------|------|------|
| **Go Agent — 核心管线** | ✅ 完成 | `cmd/agent/main.go` 支持 `schedule` 和 `server` 两种模式 |
| **数据抓取 (Fetcher)** | ✅ 完成 | RSS、NewsAPI、RSSHub 三种适配器 + Manager 并行调度 + URL 去重 |
| **AI 处理 (Processor)** | ✅ 完成 | 调用 LLM（OpenAI 兼容）进行分类、摘要、可信度评分，支持批量 10 条 |
| **来源校验 (Verifier)** | ✅ 完成 | 域名白名单 + AI 评分阈值双层策略 |
| **发布器 (Publisher)** | ✅ 完成 | HTTP POST 到网站 API，支持 3 次指数退避重试 |
| **对话接口 (Chat)** | ✅ 完成 | `POST /api/chat` + 流式 SSE `POST /api/chat/stream` + 速率限制 |
| **Agent 会话管理** | ✅ 完成 | 多轮对话上下文管理，流式响应，Tool calling |
| **PostgreSQL 存储** | ✅ 完成 | 文章 CRUD、运行日志、统计查询、全文搜索 |
| **REST 管理 API** | ✅ 完成 | 文章列表/详情/发布/重试/删除、手动触发抓取、统计接口 |
| **邮件通知 (Notifier)** | ✅ 完成 | SMTP 发送每日摘要邮件 |
| **前端 Web 应用** | ✅ 完成 | React + TypeScript + shadcn/ui，含问答、文章管理、统计、设置面板 |
| **CI/CD** | ✅ 完成 | GitHub Actions: CI 测试 + 每日定时抓取 |
| **数据库迁移** | ✅ 完成 | 3 个 migration 文件（文章表、运行日志表、FTS 索引） |

### 1.2 与原始文档的差异

项目从 v1.0 设计演化至今，以下方面已发生重大变化，原始文档（01-PRD.md ~ 04-ROADMAP.md）已严重过时：

| 原始设计 (v1.0) | 当前实现 | 影响 |
|----------------|---------|------|
| `DEEPSEEK_API_KEY` / `DEEPSEEK_MODEL_ID` | `LLM_API_KEY` / `LLM_MODEL_ID` | 环境变量名变更，可对接任意 OpenAI 兼容 API |
| SQLite 本地存储 | **PostgreSQL** (远程数据库) | 持久化方案升级，支持复杂查询和全文搜索 |
| `internal/scheduler` 主导管线 | `internal/agent.Runner` + `scheduler.Scheduler` | 架构重构，Agent 层接管编排逻辑 |
| 去重 (`cache/dedup.json`) | `internal/dedup` 独立模块 | 去重逻辑模块化 |
| 无管理 API | `internal/api` 完整 REST API | 新增了一整套管理接口 |
| 无前端界面 | `web/` React 应用 | 已具备完整 GUI |
| 无通知系统 | `internal/notifier` SMTP 通知 | 新增邮件摘要能力 |
| 无数据库迁移 | `migrations/` 3 个迁移文件 | 数据库版本管理已就绪 |
| 无速率限制 | `internal/chat/ratelimit` | 已实现每 IP 速率控制 |
| 无流式响应 | `internal/chat/stream_handler` | SSE 流式支持就绪 |

---

## 2. 阶段性规划

### Phase 1 — 文档同步与基线加固（当前阶段，1-2 周）

**目标**: 使文档与代码一致，确保所有基础设施稳定可靠。

| 任务 | 描述 | 优先级 | 预估工时 |
|------|------|--------|----------|
| 1.1 更新 PRD | 将环境变量名、架构描述、API 契约与当前代码对齐 | P0 | 4h |
| 1.2 更新 DESIGN.md | 更新模块接口、数据流图、配置表反映当前架构 | P0 | 4h |
| 1.3 更新 ROADMAP | 用当前实际进展重写路线图 | P0 | 2h |
| 1.4 更新 DEV-GUIDE | 补全 PostgreSQL 设置、新命令、环境变量参考 | P0 | 2h |
| 1.5 `.env.example` 同步 | 确保 `.env.example` 与 `config.Load()` 完全一致 | P0 | 1h |
| 1.6 补全单元测试 | 提升 `internal/agent`, `internal/api`, `internal/store` 覆盖率 | P1 | 8h |
| 1.7 集成测试加固 | 完善 `test/integration/pipeline_test.go`，覆盖更多场景 | P1 | 4h |

### Phase 2 — 部署就绪（2-3 周）

**目标**: 将系统部署到生产环境，实现 08:00 自动抓取发布。

| 任务 | 描述 | 优先级 | 预估工时 |
|------|------|--------|----------|
| 2.1 PostgreSQL 生产部署 | 购买/配置云数据库（如 Supabase / RDS），执行迁移 | P0 | 2h |
| 2.2 GitHub Secrets 配置 | 填入所有生产环境密钥（LLM_KEY, NEWSAPI_KEY, 数据库 DSN 等） | P0 | 1h |
| 2.3 首次端到端联调 | 手动触发 workflow_dispatch，验证抓取→处理→存储→发布全流程 | P0 | 4h |
| 2.4 监控抓取结果 | 连续 3 天观察定时任务的结果，确保稳定性 | P0 | 3 天观察 |
| 2.5 网站 API 对接 | 与 Java Spring Boot 网站联调 `POST /api/agent/articles` | P1 | 4h |
| 2.6 邮件通知验证 | 配置 SMTP，验证每日摘要邮件送达 | P1 | 2h |

### Phase 3 — 产品功能增强（3-5 周）

**目标**: 补齐关键体验缺口，提升信息获取质量。

| 任务 | 描述 | 优先级 | 预估工时 |
|------|------|--------|----------|
| 3.1 文章详情页 | 前端接入 `GET /api/articles/:id`，显示完整文章+摘要 | P1 | 4h |
| 3.2 手动推送/重试 | 前端接入 `POST /api/articles/:id/publish` 和重试按钮 | P1 | 3h |
| 3.3 手动触发抓取 | 前端接入 `POST /api/fetch`，支持指定分类 | P1 | 3h |
| 3.4 抓取进度展示 | 前端对接 SSE `GET /api/fetch/stream`，实时显示进度 | P1 | 4h |
| 3.5 前端过滤/搜索 | 分类筛选、状态筛选、关键词搜索文章列表 | P1 | 4h |
| 3.6 统计看板增强 | 图表可视化：每日发布趋势、来源分布饼图、分类分布 | P2 | 6h |
| 3.7 多轮对话改进 | 会话历史持久化（存数据库而非内存），重启后上下文不丢 | P2 | 6h |
| 3.8 源管理页面 | 前端配置 RSS 源、RSSHub 路由、可信域名（写环境变量或数据库） | P2 | 8h |

### Phase 4 — 内容质量提升（3-4 周）

**目标**: 提高信息信噪比，减少重复，扩展覆盖范围。

| 任务 | 描述 | 优先级 | 预估工时 |
|------|------|--------|----------|
| 4.1 语义去重 | 在 URL 去重基础上，增加基于标题/内容的语义相似度去重 | P1 | 8h |
| 4.2 更多数据源 | 接入更多 RSS 源（如 FT, WSJ, Nikkei 中文版） | P1 | 3h |
| 4.3 自定义分类 | 允许用户通过 API 或配置自定义分类，而不仅是预置 5 类 | P2 | 6h |
| 4.4 AI 摘要质量优化 | 调优 prompt，增加关键数据/引用的保留，减少套话 | P2 | 4h |
| 4.5 多语言支持 | 对非中文源自动翻译标题/摘要，保持中文输出 | P2 | 6h |
| 4.6 源可用性监控 | 检测 RSS 源是否失效，自动告警或停用 | P2 | 4h |

### Phase 5 — 运维与可观测性（2-3 周）

**目标**: 让系统运行状态可视可控，降低排障成本。

| 任务 | 描述 | 优先级 | 预估工时 |
|------|------|--------|----------|
| 5.1 Prometheus 指标 | `/metrics` 端点暴露抓取数量、延迟、LLM 调用次数等指标 | P2 | 6h |
| 5.2 Grafana 面板 | 配置看板展示每日运行状态 | P2 | 4h |
| 5.3 结构化告警 | 连续失败 N 次或抓取量为 0 时发送告警（邮件/飞书/webhook） | P2 | 6h |
| 5.4 运行历史页面 | 前端展示每次定时运行的结果（成功/失败/耗时/数量） | P2 | 6h |
| 5.5 健康检查增强 | `/health` 返回数据库连接、上次运行时间、LLM 可用性 | P2 | 2h |

### Phase 6 — 扩展与远期（长期）

| 任务 | 描述 | 优先级 |
|------|------|--------|
| 6.1 Tauri 桌面客户端 | 复用 Web GUI，打包为桌面应用 | P3 |
| 6.2 Twitter/X / Telegram 源 | 通过 RSSHub 或直接 API 接入社交平台信息 | P3 |
| 6.3 早晚双份摘要 | 除早间 08:00 外，增加晚间 20:00 摘要 | P3 |
| 6.4 个性化偏好 | 基于用户反馈学习偏好分类和源 | P3 |
| 6.5 本地 LLM 回退 | Ollama / llama.cpp 作为 DeepSeek API 不可用时的备用 | P3 |

---

## 3. 关键风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| LLM API 不可用 | 整个管线停摆 | 已实现 graceful degradation（降级为空摘要），后续可加入本地 LLM 回退 |
| RSS 源频繁变更/失效 | 抓取量下降 | 添加源健康监控，自动停用失效源 |
| PostgreSQL 连接超时 | 文章无法落库 | 实现连接池和重试，GitHub Actions 内使用 `pgbouncer` 或外部托管服务 |
| NewsAPI 免费额度耗尽 | 少一个数据源 | 监控 API 调用量，为主数据源配 RSS 备选 |
| GitHub Actions 15 分钟超时 | 处理不完整 | 当前 pipeline 已设 15 分钟 timeout，可按需分批或延长 |

---

## 4. 推荐优先级

```
Phase 1 (文档+加固) ──→ Phase 2 (部署上线) ──→ Phase 3 (产品功能) ──→ Phase 4 (内容质量)
       │                       │                       │
       ▼                       ▼                       ▼
   1-2 周                   2-3 周                   3-5 周
```

**建议立即开始的 3 件事**:
1. 文档同步（Phase 1.1 ~ 1.5）— 避免团队/新人对代码产生误解
2. 补全 `internal/agent` 和 `internal/api` 的单元测试（Phase 1.6）
3. 执行一次完整的手动调度（`--mode=schedule`），确认端到端工作正常（Phase 2.3 前置）

---

## 5. 环境变量参考（当前实际）

| 变量 | 必需 | 默认值 | 说明 |
|------|------|--------|------|
| `LLM_API_KEY` | ✅ | — | LLM API 密钥（原 DEEPSEEK_API_KEY） |
| `LLM_MODEL_ID` | ✅ | — | 模型 ID，如 `deepseek-chat` |
| `LLM_BASE_URL` | ❌ | `https://api.deepseek.com/v1` | API 基础 URL |
| `NEWSAPI_KEY` | ✅ | — | NewsAPI v2 密钥 |
| `RSSHUB_BASE_URL` | ❌ | `https://rsshub.app` | RSSHub 实例地址 |
| `RSS_FEEDS` | ❌ | 内建列表 | 分号分隔的 RSS URL 列表 |
| `RSSHUB_ROUTES` | ❌ | 内建路由列表 | 分号分隔的 RSSHub 路由路径 |
| `TRUSTED_DOMAINS` | ❌ | 内建列表 | 逗号分隔的可信域名白名单 |
| `DEFAULT_CATEGORIES` | ❌ | 全部分类 | 逗号分隔的默认分类 |
| `WEBSITE_API_BASE_URL` | ❌ | — | 网站 API 地址（空则禁用发布） |
| `WEBSITE_API_TOKEN` | ❌ | — | 网站 API Bearer Token |
| `DATABASE_DSN` | ❌ | — | PostgreSQL DSN（空则禁用持久化） |
| `SMTP_HOST` / `SMTP_USER` / `SMTP_PASSWORD` | ❌ | — | SMTP 邮件配置（空则禁用通知） |
| `NOTIFY_EMAIL` | ❌ | — | 通知接收邮箱 |
| `CHAT_API_TOKEN` | ❌ | — | Chat API 鉴权 Token |
| `CHAT_RATE_LIMIT_PER_MIN` | ❌ | 0（不限） | 每 IP 每分钟聊天请求上限 |
| `SKIP_VERIFICATION` | ❌ | `false` | 跳过来源校验（调试用） |
| `BIND_ADDR` | ❌ | `127.0.0.1:8080` | HTTP Server 监听地址 |
| `LOG_LEVEL` | ❌ | `INFO` | 日志级别 |
| `CACHE_FILE_PATH` | ❌ | `cache/dedup.json` | 去重缓存文件路径 |

---

## 6. 当前分支说明

本规划文档在独立分支 `docs/next-steps-planning` 上编写，评审通过后合并到 `main`。

后续开发请基于 `main` 创建特性分支，命名规范：
- `docs/*` — 文档更新
- `feat/*` — 新功能
- `fix/*` — 缺陷修复
- `chore/*` — 工程配置/CI/重构

---

*End of Next Steps Planning v1.0*
