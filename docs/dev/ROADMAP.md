# 04 — 项目规划与演进路线

## 1. 整体架构

系统由三个独立部分组成，Go Agent 是核心，另外两端分别面向读者和自己：

```
                    ┌─────────────────────┐
                    │     Go Agent        │
                    │  定时抓取 / AI处理   │
                    │  来源校验 / 发布     │
                    └──────┬──────────────┘
                           │
               ┌───────────┴───────────┐
               │                       │
               ▼                       ▼
   ┌─────────────────────┐   ┌─────────────────────┐
   │     个人网站         │   │      管理 GUI        │
   │  Java + React/TS    │   │   React Web（规划中）  │
   │  公开内容展示        │   │   私有管理 + 对话     │
   └─────────────────────┘   └─────────────────────┘
       面向读者                   面向自己
```

**职责边界**：

| 模块 | 职责 | 访问者 |
|---|---|---|
| Go Agent | 数据抓取、AI 处理、校验、发布、对话接口 | 内部服务 |
| 个人网站 | 展示已发布的文章内容 | 公开读者 |
| 管理 GUI | 查看所有内容、手动管理、触发抓取、对话 | 仅自己 |

---

## 2. 管理 GUI 技术选型

| 方案 | 开发成本 | 跨平台 | 说明 |
|---|---|---|---|
| **Web 应用（推荐）** | 低 | 浏览器即可 | React 基础可复用，开发最快 |
| 小程序 | 中 | 微信生态 | 限制多，不适合管理工具场景 |
| 桌面 App（Tauri） | 中 | Win/Mac/Linux | 后期可将 Web 套壳打包，无需重写 |
| 手机 App（RN/Flutter） | 高 | iOS/Android | 管理场景手机不方便，暂不优先 |

**演进路径**：先做 Web → 稳定后按需套 Tauri 打包桌面端 / React Native 做移动端，API 不变。

---

## 3. Go Agent 待补充的 API

当前 Agent 只有 `/api/chat` 和 `/health`，GUI 所需的完整接口规划如下：

### 3.1 文章管理

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/articles` | 查询文章列表（支持分类/状态/分页筛选） |
| `GET` | `/api/articles/:id` | 文章详情 |
| `POST` | `/api/articles/:id/publish` | 手动推送到个人网站 |
| `DELETE` | `/api/articles/:id` | 删除文章 |

### 3.2 抓取控制

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/fetch` | 手动触发一次全量抓取 |
| `POST` | `/api/fetch/:category` | 抓取指定板块 |

### 3.3 对话

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/chat` | 对话触发（已有），解析意图后抓取 |

### 3.4 统计

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/stats` | 今日抓取数、发布数、跳过数、来源分布 |

---

## 4. 存储层补充

当前 Agent 只有去重用的 `cache/dedup.json`，没有完整的文章持久化。GUI 要查询历史数据，需要补充本地数据库。

**选型：SQLite**
- 无需独立数据库服务，单文件，适合个人工具
- Go 库：`github.com/mattn/go-sqlite3` 或 `modernc.org/sqlite`（纯 Go，无 CGO）

**核心表设计**（详细 schema 在 `docs/02-DESIGN.md` 中补充）：

```
articles
  id, title, summary, source_url, source_domain,
  category, credibility_score, status(pending/published/skipped),
  fetched_at, published_at
```

---

## 5. 分阶段开发计划

### Phase 1 — 跑通核心链路（当前阶段）

- [ ] 安装 Go，`go mod tidy` 验证编译
- [ ] 配置 `.env`，本地启动 server 模式
- [ ] `go test ./...` 通过单元测试
- [ ] 手动触发 schedule 模式，验证抓取日志

### Phase 2 — 补完后端

- [ ] 接入真实 DeepSeek API Key，填写 model ID
- [ ] 补充 SQLite 存储层，文章落库
- [ ] 扩展管理 REST API（第 3 节所列接口）
- [ ] Java 网站侧实现 `POST /api/agent/articles` 接口
- [ ] 联调 Agent → 网站发布全流程

### Phase 3 — 管理 GUI（Web）

- [ ] 新建 React + TypeScript 前端项目（或集成进现有网站的 `/admin` 路由）
- [ ] 实现文章列表页（分类筛选、状态标记）
- [ ] 实现对话页（调用 `/api/chat`）
- [ ] 实现手动触发抓取、手动推送、删除
- [ ] 统计看板（今日数据、来源分布）

### Phase 4 — 部署与自动化

- [ ] 购买域名，部署个人网站到云服务器
- [ ] 部署 Go Agent 到云服务器（或 VPS）
- [ ] 配置 GitHub Actions Secrets，验证定时任务自动运行
- [ ] GUI 部署（同域 nginx 反代，或 Vercel/Cloudflare Pages）

### Phase 5 — 可选扩展

- [ ] Tauri 打包桌面端（复用 Web GUI 代码）
- [ ] 接入更多数据源（Telegram 频道、X/Twitter 等）
- [ ] 文章去重优化（语义相似度而非 URL 去重）
- [ ] 邮件/推送通知（每日摘要发到邮箱）

---

## 6. 各模块仓库建议

| 模块 | 建议位置 |
|---|---|
| Go Agent | 独立仓库（当前项目）|
| 个人网站 | 独立仓库（已有）|
| 管理 GUI | 与个人网站同仓库的 `/admin` 子应用，或独立仓库均可 |
