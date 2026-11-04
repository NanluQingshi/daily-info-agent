# Daily Info Agent

每日自动从互联网抓取新闻，经 AI 分类摘要与来源校验后，发布到个人网站。同时提供对话接口，支持按需抓取指定板块。

## 运行模式

| 模式 | 触发方式 | 用途 |
|---|---|---|
| `schedule` | GitHub Actions cron（每天 8am）| 定时抓取默认板块 |
| `server` | 手动启动 | 提供 HTTP 对话接口，按需触发抓取 |

## 运行健康与失败告警

定时任务最危险的失败不是「报错退出」，而是「退出码 0，但什么都没抓到」。
`internal/health` 用一份跨进程共享的 `cache/run-health.json` 记录每次运行的
结果，专门堵这个洞：

- **三种结果**：`success`（正常抓到内容）、`empty`（零抓取或低于
  `MIN_EXPECTED_ITEMS`）、`failure`（fatal error，或所有条目都没拿到 AI 结果）。
- **连续失败计数**：空跑和报错都算失败，连续失败达到
  `FAILURE_ALERT_THRESHOLD` 时通过所有已启用渠道告警。第一次失败也会立即告警。
- **新鲜度闸门**：本次运行没有产出（`empty`/`failure`）时，即使进程退出 0 也会
  以退出码 1 结束；距上次成功非空运行超过 24 小时同样失败。新鲜度基线
  （`last_success_finished_at`）不会被失败运行覆盖，所以一次空跑不会让后续
  运行永久卡死——下一次成功会正常重置计数。
- **跨进程持久化**：GitHub Actions 的 cache 同时缓存 `cache/dedup.json` 与
  `cache/run-health.json`，否则每次都是新进程、计数永远归零。
- **`/health` 端点**：返回 503 表示不健康，`freshness` 字段给出最近一次
  成功运行的时间与连续失败次数。
- **分阶段计数**：`run_logs` 的 `total_llm_skipped` 单独统计 AI 没有返回结果的
  条目；源抓取失败计入 `total_failed`。「抓取挂了」「AI 挂了」「验证没过」
  三种故障不会混成一个数字。

相关环境变量见 `.env.example` 的 `FAILURE_ALERT_THRESHOLD` 与
`MIN_EXPECTED_ITEMS`。

## 技术栈

- **语言**：Go 1.22+
- **AI**：DeepSeek API（OpenAI 兼容格式）
- **数据源**：RSS feeds、NewsAPI、RSSHub（微信公众号）
- **调度**：GitHub Actions
- **发布目标**：Java Spring Boot 网站 API

## 快速开始

```bash
# 1. 安装依赖
go mod tidy

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，填入 API keys

# 3. 启动
go run ./cmd/agent --mode=server   # 对话模式
go run ./cmd/agent --mode=schedule # 定时抓取模式（一次性）
```

详见 [开发指南](docs/03-DEV-GUIDE.md)。

## Docker 一键部署（含中文全文检索）

`docker compose up -d` 使用 `docker/postgres-zhparser/` 构建的 PostgreSQL 16 镜像：
内置 [zhparser](https://github.com/amosbird/zhparser) 扩展，中文按词切分（迁移 006 建
`zh` 配置并重建 `search_tsv`），英文按旧行为不变。数据卷首次初始化时由 initdb 脚本以
超级用户安装扩展；随后 Agent 启动即自动执行全部迁移。

```bash
cp .env.example .env && $EDITOR .env   # LLM_API_KEY / LLM_MODEL_ID 必填
docker compose up -d                   # 构建镜像 + 起库 + 起服务
```

> 自建 PostgreSQL 时需先以超级用户执行 `CREATE EXTENSION zhparser;`（或直接复用上述
> Dockerfile），再启动 Agent 迁移。

## 文档索引

| 编号 | 文档 | 内容 |
|---|---|---|
| 01 | [docs/01-PRD.md](docs/01-PRD.md) | 产品需求：用户故事、功能需求、验收标准、Java 侧 API 接口规范 |
| 02 | [docs/02-DESIGN.md](docs/02-DESIGN.md) | 技术设计：系统架构、模块接口、数据模型、错误处理策略 |
| 03 | [docs/03-DEV-GUIDE.md](docs/03-DEV-GUIDE.md) | 开发指南：环境搭建、配置说明、启动方式、测试、常用命令 |
| 04 | [docs/04-ROADMAP.md](docs/04-ROADMAP.md) | 项目规划：整体架构、GUI 选型、待补充 API、分阶段开发计划 |
| — | [CHANGELOG.md](CHANGELOG.md) | 版本发布历史 |
| — | [CONTRIBUTING.md](CONTRIBUTING.md) | 贡献指南 |

## 项目结构

```
daily-info-agent/
├── cmd/agent/main.go              # 程序入口
├── internal/
│   ├── agent/                     # LLM Agent（会话管理、流式响应、工具调用）
│   ├── api/                       # REST 管理 API（文章 CRUD、抓取触发、统计）
│   ├── chat/                      # 对话 HTTP handler（鉴权、限流、SSE 流式）
│   ├── dedup/                     # 标题级近似去重（Jaccard + Union-Find）
│   ├── fetcher/                   # 数据抓取（RSS / NewsAPI / RSSHub / 搜索引擎）
│   ├── health/                    # 跨进程运行健康信号（失败/空跑/新鲜度判定）
│   ├── notifier/                  # 邮件通知（SMTP HTML 摘要）
│   ├── processor/                 # AI 批处理（分类 + 摘要 + 可信度评分）
│   ├── publisher/                 # 发布到网站 API（带指数退避重试）
│   ├── scheduler/                 # 流水线编排
│   ├── store/                     # PostgreSQL 持久化（文章/运行日志/统计）
│   └── verifier/                  # 来源可信度校验（域名白名单 + AI 评分阈值）
├── pkg/
│   ├── backoff/                   # 指数退避重试工具
│   ├── config/                    # 环境变量加载与校验
│   └── models/                    # 共享数据结构与类型
├── web/                           # React 前端（Vite + shadcn/ui）
├── migrations/                    # SQL 数据库迁移
├── test/integration/              # 端到端集成测试
├── .github/workflows/             # GitHub Actions 调度配置
├── docs/                          # 项目文档（见上方文档索引）
├── .env.example                   # 环境变量模板
├── .dockerignore                  # Docker 构建上下文排除
├── Dockerfile                     # 多阶段 Docker 构建
├── docker-compose.yml             # 容器编排（PostgreSQL + Agent）
├── docker/postgres-zhparser/      # 带 zhparser 中文分词扩展的 PG 镜像（#55）
├── CHANGELOG.md                   # 版本发布历史
├── CONTRIBUTING.md                # 贡献指南
└── Makefile
```
