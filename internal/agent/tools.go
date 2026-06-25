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

// toolDefs is the list of tools exposed to the LLM.
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
						"description": "新闻分类"
					}
				},
				"required": ["keywords", "category"]
			}`),
		},
	},
}

// searchNewsArgs is the parsed argument struct for the search_news tool.
type searchNewsArgs struct {
	Keywords []string `json:"keywords"`
	Category string   `json:"category"`
}

// toolExecutor runs tool calls requested by the LLM and returns
// (toolResultContent, sourcesFound).
type toolExecutor struct {
	mgr     *fetcher.Manager
	maxNews int
}

// newToolExecutor creates an executor backed by the given fetcher manager.
func newToolExecutor(mgr *fetcher.Manager) *toolExecutor {
	return &toolExecutor{mgr: mgr, maxNews: 10}
}

// Execute dispatches a single ToolCall and returns the result as a string
// the LLM can read, plus any raw items that were fetched.
func (e *toolExecutor) Execute(ctx context.Context, tc openai.ToolCall) (result string, items []models.RawItem) {
	switch tc.Function.Name {
	case "get_current_time":
		return getCurrentTime(), nil
	case "search_news":
		return e.searchNews(ctx, tc.Function.Arguments)
	default:
		return fmt.Sprintf("unknown tool: %s", tc.Function.Name), nil
	}
}

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

	items, err := e.mgr.FetchForTopic(ctx, args.Keywords, e.maxNews)
	if err != nil || len(items) == 0 {
		return "未找到相关新闻", nil
	}

	return formatItems(items), items
}

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

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
