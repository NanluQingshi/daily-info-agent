# Daily Info Agent

每日自动从互联网抓取新闻，经 AI 分类摘要与来源校验后，发布到个人网站。同时提供对话接口，支持按需抓取指定板块。

## 运行模式

| 模式 | 触发方式 | 用途 |
|---|---|---|
| `schedule` | GitHub Actions cron（每天 8am）| 定时抓取默认板块 |
| `server` | 手动启动 | 提供 HTTP 对话接口，按需触发抓取 |

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

## 文档索引

| 编号 | 文档 | 内容 |
|---|---|---|
| 01 | [docs/01-PRD.md](docs/01-PRD.md) | 产品需求：用户故事、功能需求、验收标准、Java 侧 API 接口规范 |
| 02 | [docs/02-DESIGN.md](docs/02-DESIGN.md) | 技术设计：系统架构、模块接口、数据模型、错误处理策略 |
| 03 | [docs/03-DEV-GUIDE.md](docs/03-DEV-GUIDE.md) | 开发指南：环境搭建、配置说明、启动方式、测试、常用命令 |
| 04 | [docs/04-ROADMAP.md](docs/04-ROADMAP.md) | 项目规划：整体架构、GUI 选型、待补充 API、分阶段开发计划 |

## 项目结构

```
daily-info-agent/
├── cmd/agent/main.go              # 程序入口
├── internal/
│   ├── fetcher/                   # 数据抓取（RSS / NewsAPI / RSSHub）
│   ├── processor/                 # AI 处理（分类 + 摘要）
│   ├── verifier/                  # 来源可信度校验
│   ├── publisher/                 # 发布到网站 API
│   ├── chat/                      # 对话 HTTP handler
│   └── scheduler/                 # 流水线编排
├── pkg/
│   ├── config/                    # 环境变量加载
│   ├── models/                    # 共享数据结构
│   └── backoff/                   # 指数退避工具
├── test/integration/              # 端到端集成测试
├── .github/workflows/             # GitHub Actions 调度配置
├── docs/                          # 项目文档（见上方文档索引）
├── .env.example                   # 环境变量模板
└── Makefile
```
