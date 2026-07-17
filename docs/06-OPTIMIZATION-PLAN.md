# 优化与修复方案 — Optimization & Fixes

**版本**: 1.0  
**日期**: 2026-07-14  
**分支**: `chore/optimization-and-fixes`  
**作者**: AtomCode

---

## 1. 分析总结

### 1.1 现状

项目已完成全部核心功能，所有测试通过，`go vet` 无报错。覆盖率整体良好，但存在明显短板。

### 1.2 发现的问题与优化点

| 编号 | 类型 | 模块 | 问题描述 | 严重程度 |
|------|------|------|----------|----------|
| F1 | 逻辑缺陷 | `internal/verifier` | `SkipReasonLowScore` 已定义但从未被使用 — 所有未通过验证的文章都被标记为 `SkipReasonNotWhitelisted`，即使实际原因是 AI 可信度分数不足 | 中 |
| F2 | 死代码 | `internal/dedup` | `jaccard()` 函数中 `union == 0` 分支不可达（已在函数开头处理了 `len(a)==0 && len(b)==0` 的情况，且 `union` 在此处不可能为 0） | 低 |
| F3 | 测试覆盖不足 | `internal/store` | 覆盖率 **29.1%** — `SaveArticles`, `SaveRunLog`, `GetRunLog`, `ListArticles`, `GetArticle`, `DeleteArticle`, `MarkPublished`, `MarkFailed`, `MarkPending`, `GetStats` 等方法缺少测试覆盖 | 中 |
| F4 | 测试覆盖不足 | `internal/fetcher` | 覆盖率 **47.4%** — `SearchFetcher` 和 `Manager` 的测试覆盖不足 | 中 |
| F5 | 代码可维护性 | `internal/scheduler` | `runPipeline()` 方法 235 行，承担了抓取→处理→校验→发布→存储→通知的完整编排逻辑，可拆分为更小的子方法 | 低 |

### 1.3 覆盖率全景

| 包 | 当前覆盖率 | 目标 |
|----|-----------|------|
| internal/verifier | 100.0% | — |
| internal/chat | 92.3% | — |
| internal/dedup | 92.1% | — |
| internal/notifier | 92.2% | — |
| internal/publisher | 91.2% | — |
| pkg/backoff | 96.2% | — |
| pkg/config | 88.3% | — |
| pkg/models | 100.0% | — |
| internal/processor | 83.2% | — |
| internal/agent | 75.8% | — |
| internal/scheduler | 69.5% | — |
| **internal/api** | **55.6%** | ≥ 65% |
| **internal/fetcher** | **47.4%** | ≥ 65% |
| **internal/store** | **29.1%** | ≥ 60% |

---

## 2. 执行方案

### Step 1 — Bug Fix: Verifier SkipReason

**问题**: `verifier.go` 中 `verify()` 方法无论文章因何原因未通过，都返回 `SkipReasonNotWhitelisted`。当文章不在白名单且 AI 可信度分数低于阈值时，应返回 `SkipReasonLowScore`。

**变更**:
- `internal/verifier/verifier.go`: 在 AI 分数不足的分支返回 `SkipReasonLowScore`
- 不需要修改 model 定义（`SkipReasonLowScore` 已存在）

### Step 2 — 移除死代码

**问题**: `dedup.go` 中 `jaccard()` 函数在 `union == 0` 时返回 0，但此分支不可达。

**变更**:
- `internal/dedup/dedup.go`: 移除 `union == 0` 的检查

### Step 3 — 提升 store 测试覆盖率

**目标**: 从 29.1% 提升至 ≥ 60%。

**新增测试用例**:
- `TestSaveArticles` — 保存文章并验证
- `TestSaveRunLog` / `TestGetRunLog` — 运行日志读写
- `TestListArticles_FilterByCategory` — 按分类筛选
- `TestListArticles_FilterByStatus` — 按状态筛选
- `TestListArticles_FilterByDate` — 按日期范围筛选
- `TestListArticles_Search` — 全文搜索
- `TestGetArticle` — 获取单篇文章
- `TestDeleteArticle` — 删除文章
- `TestMarkPublished` / `TestMarkFailed` / `TestMarkPending` — 状态变更
- `TestGetStats` — 统计查询
- `TestPing` — 连接检查

### Step 4 — 提升 fetcher 测试覆盖率

**目标**: 从 47.4% 提升至 ≥ 65%。

**新增/增强测试**:
- `TestSearchFetcher_Fetch` — SearchFetcher 抓取测试
- `TestSearchFetcher_Name` — 名称验证
- `TestParseSearchResults` — 搜索结果解析
- `TestDecodeDuckDuckGoURL` — URL 解码
- `TestDetectLanguage` — 语言检测
- `TestBuildSearchQuery` — 搜索查询构建
- `TestCatTargetDomain` — 分类目标域名
- `TestExtractSearchDomain` — 域名提取
- 增强 Manager 测试：覆盖更多边缘情况

---

## 3. 执行顺序

```
Step 1 (Bug Fix) → Step 2 (Dead Code) → Step 3 (Store Tests) → Step 4 (Fetcher Tests)
```

每步完成后执行 `go test ./...` 验证，然后 `git commit`。

---

*End of Optimization Plan v1.0*