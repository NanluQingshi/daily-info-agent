package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/user/daily-info-agent/internal/fetcher"
	"github.com/user/daily-info-agent/pkg/models"
)

// ── Store interface ────────────────────────────────────────────────────────────

// ArticleSearcher is a minimal read interface over the article store.
// *store.PostgresStore satisfies it; pass nil when the database is disabled
// (search_stored_articles will not be registered as a tool).
type ArticleSearcher interface {
	ListArticles(ctx context.Context, f models.ArticleFilter) ([]models.ArticleRow, int, error)
}

// ── Tool definitions ──────────────────────────────────────────────────────────

// toolDefs is the list of tools exposed to the LLM.
// search_stored_articles is appended at runtime only when a store is wired in.
var toolDefs = []openai.Tool{
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_current_time",
			Description: "返回当前的日期和时间（北京时间）。当用户询问现在几点、今天几号、今天星期几等与时间/日期相关的问题时调用。",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "search_news",
			Description: "搜索最新新闻文章。当用户询问时事、行业动态、具体事件等需要实时信息的问题时调用。",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"keywords": {
						"type": "array",
						"items": {"type": "string"},
						"description": "3-5 个英文搜索关键词"
					},
					"category": {
						"type": "string",
						"enum": ["金融", "政治", "经济", "科技/AI", "国际"],
						"description": "新闻分类，会作为额外关键词加入搜索以提升相关性"
					}
				},
				"required": ["keywords", "category"]
			}`),
		},
	},
}

// storedArticleTool is appended to toolDefs when a store is available.
var storedArticleTool = openai.Tool{
	Type: openai.ToolTypeFunction,
	Function: &openai.FunctionDefinition{
		Name:        "search_stored_articles",
		Description: "搜索本地数据库中由定时任务已抓取并处理的新闻文章。当用户查询过去几天的历史新闻、需要 AI 已提炼过的摘要、或实时搜索结果为空时使用。与 search_news（实时抓取）互补。",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {
					"type": "string",
					"description": "关键词，用空格分隔，匹配文章标题和摘要"
				},
				"category": {
					"type": "string",
					"enum": ["金融", "政治", "经济", "科技/AI", "国际"],
					"description": "可选，限定新闻分类"
				},
				"days": {
					"type": "integer",
					"description": "查询最近几天内的文章，默认 7",
					"minimum": 1,
					"maximum": 90
				}
			},
			"required": ["query"]
		}`),
	},
}

// ── Executor ──────────────────────────────────────────────────────────────────

// searchNewsArgs is the parsed argument struct for the search_news tool.
type searchNewsArgs struct {
	Keywords []string `json:"keywords"`
	Category string   `json:"category"`
}

// searchStoredArgs is the parsed argument struct for the search_stored_articles tool.
type searchStoredArgs struct {
	Query    string `json:"query"`
	Category string `json:"category"`
	Days     int    `json:"days"`
}

// toolExecutor runs tool calls requested by the LLM.
type toolExecutor struct {
	mgr     *fetcher.Manager
	db      ArticleSearcher // nil when database is disabled
	maxNews int
}

// newToolExecutor creates an executor backed by the given fetcher manager and
// optional article store. Pass nil for db to disable search_stored_articles.
func newToolExecutor(mgr *fetcher.Manager, db ArticleSearcher) *toolExecutor {
	return &toolExecutor{mgr: mgr, db: db, maxNews: 10}
}

// activeTools returns the tool definitions to send to the LLM, appending
// search_stored_articles only when a database store is configured.
func (e *toolExecutor) activeTools() []openai.Tool {
	if e.db != nil {
		return append(toolDefs, storedArticleTool)
	}
	return toolDefs
}

// Execute dispatches a single ToolCall and returns the result as a string
// the LLM can read, plus any raw items that were fetched (for search_news).
func (e *toolExecutor) Execute(ctx context.Context, tc openai.ToolCall) (result string, items []models.RawItem) {
	switch tc.Function.Name {
	case "get_current_time":
		return getCurrentTime(), nil
	case "search_news":
		return e.searchNews(ctx, tc.Function.Arguments)
	case "search_stored_articles":
		return e.searchStoredArticles(ctx, tc.Function.Arguments), nil
	default:
		return fmt.Sprintf("unknown tool: %s", tc.Function.Name), nil
	}
}

// ── Tool implementations ──────────────────────────────────────────────────────

// getCurrentTime returns the current Beijing time as a human-readable string.
func getCurrentTime() string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*60*60)
	}
	now := time.Now().In(loc)
	weekdays := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	return fmt.Sprintf("当前北京时间：%d年%d月%d日 %s %02d:%02d:%02d",
		now.Year(), now.Month(), now.Day(),
		weekdays[now.Weekday()],
		now.Hour(), now.Minute(), now.Second(),
	)
}

// searchNews implements the search_news tool.
func (e *toolExecutor) searchNews(ctx context.Context, argsJSON string) (string, []models.RawItem) {
	var args searchNewsArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err), nil
	}
	if len(args.Keywords) == 0 {
		return "keywords 不能为空", nil
	}

	// Map the requested category to an English topic keyword so it actually
	// biases the NewsAPI query and the local keyword filter — otherwise the
	// field was parsed and silently dropped.
	keywords := args.Keywords
	if kw, ok := categoryTopicKeyword(args.Category); ok {
		keywords = append(append([]string{}, args.Keywords...), kw)
	}

	items, err := e.mgr.FetchForTopic(ctx, keywords, e.maxNews)
	if err != nil || len(items) == 0 {
		return "未找到相关新闻", nil
	}
	return formatItems(items), items
}

// categoryTopicKeyword maps an internal category to an English search term.
// Returns ok=false for empty/unknown categories.
func categoryTopicKeyword(cat string) (string, bool) {
	switch models.Category(cat) {
	case models.CategoryFinance:
		return "finance", true
	case models.CategoryPolitics:
		return "politics", true
	case models.CategoryEconomy:
		return "economy", true
	case models.CategoryTechAI:
		return "technology AI", true
	case models.CategoryInternational:
		return "world international", true
	}
	return "", false
}

// searchStoredArticles implements the search_stored_articles tool.
// Returns "数据库未启用" when db is nil.
func (e *toolExecutor) searchStoredArticles(ctx context.Context, argsJSON string) string {
	if e.db == nil {
		return "数据库未启用，无法查询历史文章"
	}

	var args searchStoredArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}
	if args.Query == "" {
		return "query 不能为空"
	}
	if args.Days <= 0 {
		args.Days = 7
	}

	dateFrom := time.Now().UTC().AddDate(0, 0, -args.Days)

	filter := models.ArticleFilter{
		Query:    args.Query,
		DateFrom: &dateFrom,
		PageSize: 10,
		Page:     1,
	}
	if args.Category != "" {
		cat := models.Category(args.Category)
		filter.Category = &cat
	}

	rows, total, err := e.db.ListArticles(ctx, filter)
	if err != nil {
		return fmt.Sprintf("数据库查询失败: %v", err)
	}
	if len(rows) == 0 {
		return fmt.Sprintf("最近 %d 天内未找到与「%s」相关的历史文章", args.Days, args.Query)
	}

	return formatStoredArticles(rows, total, args.Days)
}

// ── Formatters ────────────────────────────────────────────────────────────────

// formatItems converts raw items into a compact text block the LLM can digest.
func formatItems(items []models.RawItem) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("找到 %d 篇相关新闻：\n\n", len(items)))
	for i, it := range items {
		sb.WriteString(fmt.Sprintf("%d. 【%s】%s\n   来源：%s\n   摘要：%s\n\n",
			i+1,
			it.SourceDomain,
			it.Title,
			it.URL,
			truncate(it.Description, 150),
		))
	}
	return sb.String()
}

// formatStoredArticles formats a list of stored ArticleRow records for the LLM.
func formatStoredArticles(rows []models.ArticleRow, total, days int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("最近 %d 天共找到 %d 篇历史文章（显示前 %d 篇）：\n\n", days, total, len(rows)))
	for i, a := range rows {
		date := a.FetchedAt.Format("2006-01-02")
		sb.WriteString(fmt.Sprintf("%d. 【%s】%s（%s）\n   来源：%s\n   摘要：%s\n\n",
			i+1,
			a.Category,
			a.Title,
			date,
			a.SourceDomain,
			truncate(a.Summary, 200),
		))
	}
	return sb.String()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
