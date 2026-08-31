// Package api — article export endpoint (GET /api/articles/export).
package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/user/daily-info-agent/pkg/models"
)

// errExportTooLarge signals the row cap was hit; the caller maps it to a 400
// with an actionable message instead of a generic db_error.
var errExportTooLarge = errors.New("export too large")

const (
	// exportPageSize is the page size used internally when paging through the
	// store for an export. It matches the store's per-query maximum.
	exportPageSize = 100

	// maxExportRows caps a single export so a huge database cannot exhaust
	// memory. The cap is announced in the error message.
	maxExportRows = 10000
)

// csvExportColumns are the columns of the CSV export, in order.
var csvExportColumns = []string{
	"id", "title", "category", "status", "credibility_score",
	"source_domain", "source_type", "language", "tags",
	"summary", "content_text", "source_url", "fetched_at", "published_at",
}

// exportTimestamp renders a nullable timestamp as CSV text.
func exportTimestamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// ExportArticles handles GET /api/articles/export?format=csv|json plus the
// same filter parameters as GET /api/articles (category, status, date_from,
// date_to, q). Pagination parameters are ignored — an export always contains
// every matching row (up to maxExportRows).
func (h *Handler) ExportArticles(c echo.Context) error {
	if err := h.requireStore(c); err != nil {
		return err
	}

	format := strings.ToLower(strings.TrimSpace(c.QueryParam("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" && format != "md" && format != "markdown" {
		return errJSON(c, http.StatusBadRequest, "invalid_param", "format must be csv, json or markdown")
	}

	f, ok := parseArticleFilter(c)
	if !ok {
		return nil
	}
	// Export ignores client pagination: pages are driven internally.
	f.Page = 1
	f.PageSize = exportPageSize

	articles, err := h.collectForExport(c.Request().Context(), f)
	if err != nil {
		if errors.Is(err, errExportTooLarge) {
			return errJSON(c, http.StatusBadRequest, "export_limited", err.Error())
		}
		h.logger.Error("export articles failed", slog.String("error", err.Error()))
		return errJSON(c, http.StatusInternalServerError, "db_error", "failed to collect articles for export")
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	switch format {
	case "md", "markdown":
		body := renderMarkdownExport(articles)
		c.Response().Header().Set(echo.HeaderContentDisposition,
			fmt.Sprintf(`attachment; filename="articles-%s.md"`, stamp))
		return c.Blob(http.StatusOK, "text/markdown; charset=utf-8", []byte(body))
	case "json":
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(articles); err != nil {
			h.logger.Error("encode export json failed", slog.String("error", err.Error()))
			return errJSON(c, http.StatusInternalServerError, "encode_error", "failed to encode articles")
		}
		c.Response().Header().Set(echo.HeaderContentType, "application/json; charset=utf-8")
		c.Response().Header().Set(echo.HeaderContentDisposition,
			fmt.Sprintf(`attachment; filename="articles-%s.json"`, stamp))
		return c.Blob(http.StatusOK, "application/json; charset=utf-8", buf.Bytes())
	default:
		var buf bytes.Buffer
		// UTF-8 BOM so Excel detects the encoding and shows Chinese correctly.
		buf.WriteString("\uFEFF")
		w := csv.NewWriter(&buf)
		if err := w.Write(csvExportColumns); err != nil {
			return errJSON(c, http.StatusInternalServerError, "encode_error", "failed to encode articles")
		}
		for _, a := range articles {
			record := []string{
				fmt.Sprintf("%d", a.ID),
				a.Title,
				string(a.Category),
				a.Status,
				fmt.Sprintf("%.2f", a.CredibilityScore),
				a.SourceDomain,
				a.SourceType,
				a.Language,
				strings.Join(a.Tags, "|"),
				a.Summary,
				a.ContentText,
				a.SourceURL,
				a.FetchedAt.UTC().Format(time.RFC3339),
				exportTimestamp(a.PublishedAt),
			}
			if err := w.Write(record); err != nil {
				return errJSON(c, http.StatusInternalServerError, "encode_error", "failed to encode articles")
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			h.logger.Error("encode export csv failed", slog.String("error", err.Error()))
			return errJSON(c, http.StatusInternalServerError, "encode_error", "failed to encode articles")
		}
		c.Response().Header().Set(echo.HeaderContentDisposition,
			fmt.Sprintf(`attachment; filename="articles-%s.csv"`, stamp))
		return c.Blob(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
	}
}

// collectForExport pages through ListArticles with the given filter until
// every matching row is gathered or maxExportRows is reached.
func (h *Handler) collectForExport(ctx context.Context, f models.ArticleFilter) ([]models.ArticleRow, error) {
	var all []models.ArticleRow
	total := -1
	for page := 1; ; page++ {
		f.Page = page
		rows, t, err := h.store.ListArticles(ctx, f)
		if err != nil {
			return nil, err
		}
		if total < 0 {
			total = t
		}
		all = append(all, rows...)
		if len(rows) < exportPageSize || len(all) >= total {
			break
		}
		if len(all) >= maxExportRows {
			return nil, fmt.Errorf("%w: more than %d matching articles; narrow the filters (category/status/date range) and retry", errExportTooLarge, maxExportRows)
		}
	}
	return all, nil
}

// mdEscape keeps heading/list syntax inside a field from breaking the
// document structure: leading marker characters are neutralised with a
// backslash escape.
func mdEscape(s string) string {
	if s == "" {
		return ""
	}
	if s[0] == '#' || s[0] == '-' || s[0] == '>' || s[0] == '|' {
		return "\\" + s
	}
	return s
}

// renderMarkdownExport renders the articles as a readable Markdown archive:
// one section per article with the complete field set.
func renderMarkdownExport(articles []models.ArticleRow) string {
	var b strings.Builder
	b.WriteString("# 文章导出\n\n")
	b.WriteString(fmt.Sprintf("- 导出时间: %s\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- 文章数: %d\n\n", len(articles)))
	for _, a := range articles {
		b.WriteString(fmt.Sprintf("## %d. %s\n\n", a.ID, a.Title))
		b.WriteString(fmt.Sprintf("- 分类: %s\n", a.Category))
		b.WriteString(fmt.Sprintf("- 状态: %s\n", a.Status))
		b.WriteString(fmt.Sprintf("- 来源: %s (%s)\n", a.SourceDomain, a.SourceType))
		b.WriteString(fmt.Sprintf("- 语言: %s\n", a.Language))
		if len(a.Tags) > 0 {
			b.WriteString(fmt.Sprintf("- 标签: %s\n", strings.Join(a.Tags, " · ")))
		}
		b.WriteString(fmt.Sprintf("- 可信度: %.2f\n", a.CredibilityScore))
		b.WriteString(fmt.Sprintf("- 链接: %s\n", a.SourceURL))
		b.WriteString(fmt.Sprintf("- 抓取时间: %s\n", a.FetchedAt.UTC().Format(time.RFC3339)))
		if a.PublishedAt != nil {
			b.WriteString(fmt.Sprintf("- 发布时间: %s\n", a.PublishedAt.UTC().Format(time.RFC3339)))
		}
		if a.Summary != "" {
			b.WriteString(fmt.Sprintf("\n**摘要**\n\n%s\n", mdEscape(a.Summary)))
		}
		body := a.ContentText
		if body == "" {
			body = a.Content // fall back to the processed content
		}
		if body != "" {
			b.WriteString(fmt.Sprintf("\n**正文**\n\n%s\n", mdEscape(body)))
		}
		b.WriteString("\n---\n\n")
	}
	return b.String()
}
