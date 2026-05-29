# 03 — 开发指南

## 1. 环境准备

### 1.1 安装 Go

项目要求 Go 1.22+。macOS 推荐用 Homebrew 安装：

```bash
brew install go
```

安装完成后重开终端，验证：

```bash
go version
# 期望输出: go version go1.22.x darwin/arm64
```

### 1.2 安装 Git（一般已有）

```bash
git --version
```

---

## 2. 项目初始化

### 2.1 进入项目目录

```bash
cd /path/to/daily-info-agent
```

### 2.2 下载依赖

```bash
go mod tidy
```

> `go mod tidy` 会自动拉取 `go.mod` 里声明的所有依赖，首次运行需要网络。

### 2.3 验证能编译

```bash
go build ./...
```

无任何报错则说明依赖完整、代码可编译。

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
| `DEEPSEEK_API_KEY` | `test-key` | 先占位，schedule 模式才真正调用 |
| `DEEPSEEK_MODEL_ID` | `deepseek-chat` | 等 DeepSeek V4 Pro 确认 model ID 后替换 |
| `DEEPSEEK_BASE_URL` | `https://api.deepseek.com/v1` | 默认值，一般不改 |
| `SKIP_VERIFICATION` | `true` | **调试关键**：跳过 AI 可信度校验 |
| `WEBSITE_API_BASE_URL` | `http://localhost:9090` | Java 侧 API 未实现前用占位 |
| `WEBSITE_API_TOKEN` | `test-token` | 占位 |
| `NEWSAPI_KEY` | 留空 | 空时 NewsAPI fetcher 自动跳过 |
| `LOG_LEVEL` | `DEBUG` | 调试期间看完整日志 |

### 3.3 生产配置（后续补充）

| 变量 | 说明 | 获取方式 |
|---|---|---|
| `DEEPSEEK_API_KEY` | DeepSeek API 密钥 | https://platform.deepseek.com/ |
| `NEWSAPI_KEY` | NewsAPI 密钥 | https://newsapi.org/register |
| `WEBSITE_API_BASE_URL` | 网站部署后的真实地址 | 自己的云服务器 |
| `WEBSITE_API_TOKEN` | 网站 API 鉴权 token | Java 侧生成后填入 |

---

## 4. 启动与运行

### 4.1 两种运行模式

| 模式 | 命令 | 用途 |
|---|---|---|
| `server` | `go run ./cmd/agent --mode=server` | 启动 HTTP 服务，支持对话触发（默认） |
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
  "time": "2026-05-29T08:00:00Z"
}
```

### 4.3 测试对话接口

```bash
curl -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "帮我抓取今天的 AI 相关新闻"}'
```

### 4.4 手动触发一次完整抓取

```bash
go run ./cmd/agent --mode=schedule
```

会依次执行：抓取 → AI 处理 → 来源校验 → 发布到网站 API，完成后退出并打印统计。

---

## 5. 测试

### 5.1 运行全部单元测试

```bash
go test ./...
```

### 5.2 运行指定包的测试

```bash
go test ./internal/verifier/...    # 来源校验
go test ./internal/fetcher/...     # 抓取器
go test ./internal/processor/...   # AI 处理
go test ./internal/publisher/...   # 发布器
```

### 5.3 查看测试覆盖率

```bash
go test ./... -cover
```

输出示例：

```
ok  github.com/user/daily-info-agent/internal/verifier   coverage: 87.5%
ok  github.com/user/daily-info-agent/internal/fetcher    coverage: 76.2%
```

### 5.4 运行集成测试

集成测试需显式指定 build tag（默认不跑，避免影响日常 CI）：

```bash
go test -tags=integration ./test/integration/...
```

---

## 6. Java 侧对接（网站 API）

Agent 发布文章时会调用 Java 网站的以下接口（详见 `docs/01-PRD.md` 第 6 节）：

```
POST /api/agent/articles
Authorization: Bearer <WEBSITE_API_TOKEN>
Content-Type: application/json
```

**Java 侧未完成前的调试方法**：启动一个本地 mock 服务监听 `9090` 端口：

```bash
# 用 nc 简单 mock，返回 201
while true; do
  echo -e "HTTP/1.1 201 Created\r\nContent-Length: 2\r\n\r\n{}" | nc -l 9090
done
```

或者在 `.env` 里设 `SKIP_VERIFICATION=true` + 任意占位 URL，在 publisher 的日志里看请求内容即可。

---

## 7. GitHub Actions 自动调度

调度配置文件：`.github/workflows/daily-fetch.yml`

- **触发时间**：每天 UTC 00:00（北京时间 08:00）
- **手动触发**：GitHub 仓库 → Actions → Daily Info Fetch → Run workflow

需在 GitHub 仓库的 **Settings → Secrets and variables → Actions** 里添加以下 secrets：

| Secret 名称 | 对应 .env 变量 |
|---|---|
| `DEEPSEEK_API_KEY` | `DEEPSEEK_API_KEY` |
| `NEWSAPI_KEY` | `NEWSAPI_KEY` |
| `WEBSITE_API_BASE_URL` | `WEBSITE_API_BASE_URL` |
| `WEBSITE_API_TOKEN` | `WEBSITE_API_TOKEN` |

---

## 8. 常用命令速查

```bash
# 依赖
go mod tidy                          # 下载/整理依赖
go mod download                      # 仅下载不修改 go.mod

# 构建
go build -o agent ./cmd/agent        # 编译成二进制
go build ./...                       # 编译全部包（验证无报错）

# 运行
go run ./cmd/agent --mode=server     # server 模式
go run ./cmd/agent --mode=schedule   # schedule 模式（一次性）

# 测试
go test ./...                        # 全部测试
go test ./... -v                     # 详细输出
go test ./... -cover                 # 含覆盖率
go test -run TestVerifier_Whitelist ./internal/verifier/...  # 跑单个测试

# 代码检查
go vet ./...                         # 静态分析
```
