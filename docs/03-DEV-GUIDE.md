# 03 — 开发指南

## 1. 环境准备

### 1.1 安装 Go

项目要求 Go 1.25+。macOS 推荐用 Homebrew 安装：

```bash
brew install go
```

安装完成后重开终端，验证：

```bash
go version
# 期望输出: go version go1.25.x darwin/arm64
```

### 1.2 安装 Node.js（前端开发）

前端位于 `web/` 目录，需要 Node.js 20+：

```bash
node --version   # >= 20
```

### 1.3 安装 PostgreSQL（可选，用于本地持久化）

```bash
brew install postgresql@16
brew services start postgresql@16
createdb daily_info
```

---

## 2. 项目初始化

### 2.1 进入项目目录

```bash
cd /path/to/daily-info-agent
```

### 2.2 下载 Go 依赖

```bash
go mod tidy
```

### 2.3 安装前端依赖

```bash
cd web && npm ci && cd ..
```

### 2.4 验证能编译

```bash
go build ./...       # Go 后端
cd web && npm run build && cd ..   # 前端
```

---

## 3. 配置

### 3.1 复制配置模板

```bash
cp .env.example .env
```

`.env` 已加入 `.gitignore`，不会提交到版本库。

### 3.2 最小调试配置

初期调试不需要所有 key 都真实，按下表填写 `.env`：

| 变量 | 调试值 | 说明 |
|---|---|---|
| `LLM_API_KEY` | `test-key` | 先占位，schedule/server 模式才真正调用 |
| `LLM_MODEL_ID` | `deepseek-chat` | 要发请求的模型 ID |
| `LLM_BASE_URL` | `https://api.deepseek.com/v1` | 默认值，可按需改为中科大等代理 |
| `NEWSAPI_KEY` | 留空 | 空时 NewsAPI fetcher 自动跳过 |
| `SKIP_VERIFICATION` | `true` | **调试关键**：跳过 AI 可信度校验 |
| `WEBSITE_API_BASE_URL` | 留空 | 留空则禁用发布（本地调试推荐） |
| `WEBSITE_API_TOKEN` | 留空 | 留空则禁用发布 |
| `DATABASE_DSN` | 留空 | 留空则不持久化，用文件缓存去重 |
| `LOG_LEVEL` | `DEBUG` | 调试期间看完整日志 |

### 3.3 生产配置

| 变量 | 说明 | 获取方式 |
|---|---|---|
| `LLM_API_KEY` | LLM API 密钥 | 各平台申请（DeepSeek / 中科大等） |
| `LLM_MODEL_ID` | 模型 ID | 根据使用的 API 平台填写 |
| `NEWSAPI_KEY` | NewsAPI 密钥 | https://newsapi.org/register |
| `DATABASE_DSN` | PostgreSQL 连接串 | 自托管或 Supabase / RDS |
| `WEBSITE_API_BASE_URL` | 网站部署后的真实地址 | 自己的云服务器 |
| `WEBSITE_API_TOKEN` | 网站 API 鉴权 token | Java 侧生成后填入 |
| `SMTP_HOST` / `SMTP_USER` / `SMTP_PASSWORD` | 邮件通知 | QQ邮箱 / Gmail / 企业邮箱 |

---

## 4. 启动与运行

### 4.1 两种运行模式

| 模式 | 命令 | 用途 |
|---|---|---|
| `server` | `go run ./cmd/agent --mode=server` | 启动 HTTP 服务，支持对话 + 管理 API（默认） |
| `schedule` | `go run ./cmd/agent --mode=schedule` | 执行一次完整定时抓取流程后退出 |

### 4.2 启动 server 模式

```bash
go run ./cmd/agent --mode=server
```

验证服务正常：

```bash
curl http://localhost:8080/health
```

期望返回：

```json
{
  "status": "ok",
  "version": "1.0.0",
  "time": "2026-07-03T07:27:00Z"
}
```

### 4.3 测试对话接口

```bash
curl -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "帮我抓取今天的 AI 相关新闻"}'
```

如果配置了 `CHAT_API_TOKEN`，需要加请求头：

```bash
curl -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -H "X-Api-Token: your-token" \
  -d '{"message": "今天 AI 芯片有什么新闻？"}'
```

### 4.4 测试流式对话接口

```bash
curl -N -X POST http://localhost:8080/api/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"message": "帮我抓取今天的 AI 相关新闻"}'
```

### 4.5 手动触发一次完整抓取

```bash
go run ./cmd/agent --mode=schedule
```

会依次执行：抓取 → AI 处理 → 来源校验 → 保存数据库 → 发布到网站 API → 发送邮件摘要，完成后退出并打印统计。

### 4.6 管理 API 示例（server 模式下可用）

```bash
# 查询文章列表
curl http://localhost:8080/api/articles?page=1&page_size=20

# 查看单篇文章
curl http://localhost:8080/api/articles/1

# 手动触发抓取
curl -X POST http://localhost:8080/api/fetch

# 抓取指定分类
curl -X POST http://localhost:8080/api/fetch/科技/AI

# 获取统计数据
curl http://localhost:8080/api/stats

# 发布文章到网站
curl -X POST http://localhost:8080/api/articles/1/publish

# 删除文章
curl -X DELETE http://localhost:8080/api/articles/1
```

### 4.7 启动前端开发服务器

```bash
cd web
npm run dev
```

前端默认运行在 `http://localhost:5173`，需要后端 server 模式同时运行在 `8080` 端口，
Vite 配置了代理，`/api/*` 请求会自动转发到后端。

---

## 5. 测试

### 5.1 运行全部单元测试

```bash
go test ./...
```

### 5.2 运行指定包的测试

```bash
go test ./internal/verifier/...   # 来源校验
go test ./internal/fetcher/...    # 抓取器
go test ./internal/processor/...  # AI 处理
go test ./internal/publisher/...  # 发布器
go test ./internal/store/...      # 数据库存储
go test ./internal/agent/...      # Agent 会话管理
go test ./internal/dedup/...      # 去重
go test ./internal/api/...        # 管理 API
go test ./internal/chat/...       # 对话接口
```

### 5.3 查看测试覆盖率

```bash
go test ./... -cover
```

输出示例：

```
ok  github.com/user/daily-info-agent/internal/verifier   coverage: 87.5%
ok  github.com/user/daily-info-agent/internal/fetcher    coverage: 76.2%
ok  github.com/user/daily-info-agent/internal/agent      coverage: 62.3%
```

### 5.4 运行集成测试

集成测试需显式指定 build tag（默认不跑，避免影响日常 CI）：

```bash
go test -tags=integration ./test/integration/...
```

### 5.5 数据库迁移测试

```bash
# 创建测试数据库（首次）
createdb daily_info_test

# 运行迁移
DATABASE_DSN="postgres://localhost:5432/daily_info_test?sslmode=disable" \
  go run ./cmd/agent --mode=schedule

# 清理
dropdb daily_info_test
```

---

## 6. 数据库

### 6.1 PostgreSQL Schema 迁移

迁移文件位于 `migrations/` 目录，使用 Go 标准库嵌入（`embed`）在 `cmd/agent/main.go` 中执行：

| 文件 | 说明 |
|---|---|
| `001_create_articles.up.sql` | 文章表、分类索引、去重唯一约束 |
| `001_create_articles.down.sql` | 回滚 |
| `002_create_run_logs.up.sql` | 运行日志表 |
| `002_create_run_logs.down.sql` | 回滚 |
| `003_add_articles_fts.up.sql` | 全文搜索索引（中文 + 英文） |
| `003_add_articles_fts.down.sql` | 回滚 |

迁移在 server 和 schedule 模式启动时自动执行，仅在 `DATABASE_DSN` 配置时生效。

### 6.2 本地 PostgreSQL 快速设置

```bash
# 创建数据库
createdb daily_info

# 直接启动（自动执行迁移）
DATABASE_DSN="postgres://$(whoami)@localhost:5432/daily_info?sslmode=disable" \
  go run ./cmd/agent --mode=server
```

### 6.3 核心表结构

**articles 表**：`id`, `title`, `summary`, `source_url`（唯一）, `source_domain`, `category`, `credibility_score`, `status`（pending/published/skipped）, `tags`, `language`, `run_id`, `fetched_at`, `published_at`, `created_at`, `updated_at`，以及 FTS 向量列。

**run_logs 表**：`id`, `run_id`, `status`, `total_fetched`, `total_processed`, `total_published`, `total_skipped`, `total_failed`, `duration_ms`, `error_message`，以及 JSON `details` 字段。

---

## 7. 前端开发

### 7.1 技术栈

| 技术 | 用途 |
|---|---|
| React 19 + TypeScript | 框架 |
| Vite | 构建工具 |
| Tailwind CSS 4 | 样式 |
| shadcn/ui | UI 组件库 |
| Radix UI | 无头组件 |
| Lucide React | 图标 |

### 7.2 项目结构

```
web/src/
├── api/
│   └── client.ts          # HTTP API 客户端
├── components/
│   ├── ArticleCard.tsx
│   ├── ArticleDetail.tsx
│   ├── ArticleList.tsx
│   ├── ChatPanel.tsx
│   ├── ChatView.tsx
│   ├── ConversationList.tsx
│   ├── FetchButton.tsx
│   ├── FilterBar.tsx
│   ├── MarkdownContent.tsx
│   ├── SettingsPanel.tsx
│   ├── StatsPanel.tsx
│   └── ui/                 # shadcn/ui 组件
├── hooks/
├── lib/
│   └── utils.ts
├── types/
│   └── index.ts
├── App.tsx
└── main.tsx
```

### 7.3 构建前端

```bash
cd web
npm run build       # 生产构建，输出到 web/dist/
```

构建完成后，server 模式会自动在 `http://localhost:8080` 上静态服务前端。

---

## 8. Java 侧对接（网站 API）

Agent 发布文章时会调用 Java 网站的以下接口（详见 `docs/01-PRD.md` 第 6 节）：

```
POST /api/agent/articles
Authorization: Bearer <WEBSITE_API_TOKEN>
Content-Type: application/json
```

**Java 侧未完成前的调试方法**：在 `.env` 里留空 `WEBSITE_API_BASE_URL` 和 `WEBSITE_API_TOKEN`，publisher 会自动跳过。
数据会存入本地 PostgreSQL（如果配置了 `DATABASE_DSN`），通过前端管理界面可直接查看。

---

## 9. GitHub Actions 自动调度

### 9.1 定时抓取

调度配置文件：`.github/workflows/daily-fetch.yml`

- **触发时间**：每天 UTC 01:00（北京时间 09:00）
- **手动触发**：GitHub 仓库 → Actions → 每日新闻抓取 → Run workflow
- **支持参数**：可输入 `categories` 指定要抓取的分类（逗号分隔）

### 9.2 CI 检查

配置文件：`.github/workflows/ci.yml`

- PR 和 push 到 `main` 时自动运行
- 检查项：`go vet` + 单元测试 + 前端构建

### 9.3 需配置的 GitHub Secrets

| Secret 名称 | 对应 .env 变量 | 必需 |
|---|---|---|
| `LLM_API_KEY` | `LLM_API_KEY` | ✅ |
| `LLM_MODEL_ID` | `LLM_MODEL_ID` | ✅ |
| `NEWSAPI_KEY` | `NEWSAPI_KEY` | ✅ |
| `DATABASE_DSN` | `DATABASE_DSN` | ❌（可选） |
| `WEBSITE_API_BASE_URL` | `WEBSITE_API_BASE_URL` | ❌（可选） |
| `WEBSITE_API_TOKEN` | `WEBSITE_API_TOKEN` | ❌（可选） |
| `SMTP_HOST` / `SMTP_USER` / `SMTP_PASSWORD` | SMTP 相关 | ❌（可选） |
| `NOTIFY_EMAIL` | `NOTIFY_EMAIL` | ❌（可选） |

### 9.4 需配置的 GitHub Variables（非敏感）

| Variable 名称 | 对应 .env 变量 | 说明 |
|---|---|---|
| `RSSHUB_BASE_URL` | `RSSHUB_BASE_URL` | RSSHub 实例地址 |
| `RSS_FEEDS` | `RSS_FEEDS` | 自定义 RSS 源 |
| `TRUSTED_DOMAINS` | `TRUSTED_DOMAINS` | 可信域名白名单 |
| `DEFAULT_CATEGORIES` | `DEFAULT_CATEGORIES` | 默认分类列表 |

---

## 10. 常用命令速查

```bash
# === Go 后端 ===

# 依赖
go mod tidy                          # 下载/整理依赖
go mod download                      # 仅下载不修改 go.mod

# 构建
go build -o agent ./cmd/agent        # 编译成二进制
go build ./...                       # 编译全部包（验证无报错）

# 运行
go run ./cmd/agent --mode=server     # server 模式（加 -race 检查竞态）
go run ./cmd/agent --mode=schedule   # schedule 模式（一次性）

# 测试
go test ./...                        # 全部测试
go test ./... -v                     # 详细输出
go test ./... -cover                 # 含覆盖率
go test -run TestXXX ./internal/...  # 跑单个测试
go test -tags=integration ./test/... # 集成测试
go vet ./...                         # 静态分析

# === 前端 ===

cd web
npm run dev                          # 开发服务器
npm run build                        # 生产构建
npx tsc --noEmit                     # TypeScript 类型检查
npx prettier --check src/            # 格式检查

# === 数据库 ===

# 迁移（启动时自动执行，也可手动）
DATABASE_DSN="postgres://localhost:5432/daily_info?sslmode=disable" \
  go run ./cmd/agent --mode=schedule

# 直接查询
psql daily_info
\d articles
SELECT count(*), status FROM articles GROUP BY status;
```
