package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

// pageOf builds a filter-aware stub that serves rows [from, to) in pages of
// exportPageSize, mimicking the real store's pagination contract.
func pageOf(rows []models.ArticleRow) func(models.ArticleFilter) ([]models.ArticleRow, int, error) {
	return func(f models.ArticleFilter) ([]models.ArticleRow, int, error) {
		page := f.Page
		if page < 1 {
			page = 1
		}
		size := f.PageSize
		if size < 1 {
			size = 20
		}
		from := (page - 1) * size
		if from >= len(rows) {
			return []models.ArticleRow{}, len(rows), nil
		}
		to := from + size
		if to > len(rows) {
			to = len(rows)
		}
		return rows[from:to], len(rows), nil
	}
}

// ---------------------------------------------------------------------------
// ExportArticles — CSV
// ---------------------------------------------------------------------------

func TestExportArticles_CSV_Default(t *testing.T) {
	pub := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	rows := []models.ArticleRow{
		{
			ID: 1, Title: "国产芯片新突破", Category: models.CategoryTechAI, Status: "published",
			CredibilityScore: 0.95, SourceDomain: "example.com", SourceType: "rss", Language: "zh",
			Tags: []string{"芯片", "半导体"}, Summary: "一条,带逗号的摘要", SourceURL: "https://example.com/a",
			FetchedAt: pub, PublishedAt: &pub,
		},
		{
			ID: 2, Title: `He said "hello"`, Category: models.CategoryFinance, Status: "pending",
			SourceDomain: "reuters.com", SourceType: "rss", Language: "en",
			Summary: "plain", ContentText: "正文全文", SourceURL: "https://reuters.com/b",
			FetchedAt: pub,
		},
	}
	m := &mockStore{}
	m.listFn = pageOf(rows)
	h := newHandler(m, nil, nil)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/articles/export", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.ExportArticles(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	// UTF-8 BOM so Excel renders Chinese correctly.
	assert.True(t, strings.HasPrefix(body, "\uFEFF"), "CSV must start with UTF-8 BOM")

	ct := rec.Header().Get("Content-Type")
	assert.Contains(t, ct, "text/csv")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `attachment; filename="articles-`)

	lines := strings.Split(strings.TrimPrefix(body, "\uFEFF"), "\n")
	assert.Equal(t, "id,title,category,status,credibility_score,source_domain,source_type,language,tags,summary,content_text,source_url,fetched_at,published_at", lines[0])

	// Row 1: CJK content, comma inside the summary, tags joined with |, full text column.
	row1, err := parseCSVLine(lines[1])
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "国产芯片新突破", "科技/AI", "published", "0.95", "example.com", "rss", "zh", "芯片|半导体", "一条,带逗号的摘要", "", "https://example.com/a", pub.Format(time.RFC3339), pub.Format(time.RFC3339)}, row1)

	// Row 2: quotes escaped per RFC 4180, no published_at.
	row2, err := parseCSVLine(lines[2])
	require.NoError(t, err)
	assert.Equal(t, "2", row2[0])
	assert.Equal(t, `He said "hello"`, row2[1])
	assert.Equal(t, "正文全文", row2[10]) // content_text
	assert.Equal(t, "", row2[13])     // published_at empty
}

// parseCSVLine decodes a single CSV record (a physical line; sufficient for
// the generated fixture above since no field contains a newline).
func parseCSVLine(line string) ([]string, error) {
	r := csv.NewReader(strings.NewReader(line + "\n"))
	r.FieldsPerRecord = -1
	return r.Read()
}

func TestExportArticles_CSV_ExplicitFormatParam(t *testing.T) {
	m := &mockStore{}
	m.listFn = pageOf(nil)
	h := newHandler(m, nil, nil)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/articles/export?format=CSV", nil) // case-insensitive
	rec := httptest.NewRecorder()
	require.NoError(t, h.ExportArticles(e.NewContext(req, rec)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
}

func TestExportArticles_Markdown(t *testing.T) {
	pub := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	rows := []models.ArticleRow{
		{
			ID: 3, Title: "芯片量产", Category: models.CategoryTechAI, Status: "published",
			CredibilityScore: 0.9, SourceDomain: "xinhua.net", SourceType: "rss", Language: "zh",
			Tags: []string{"芯片"}, Summary: "- 看似列表的摘要", ContentText: "正文内容",
			SourceURL: "https://xinhua.net/c", FetchedAt: pub, PublishedAt: &pub,
		},
		{
			// No content_text → falls back to processed content; empty summary omitted.
			ID: 4, Title: "第二条", Category: models.CategoryFinance, Status: "pending",
			SourceDomain: "reuters.com", SourceType: "rss", Language: "en",
			Content: "fallback body", SourceURL: "https://reuters.com/d", FetchedAt: pub,
		},
	}
	m := &mockStore{}
	m.listFn = pageOf(rows)
	h := newHandler(m, nil, nil)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/articles/export?format=markdown", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.ExportArticles(e.NewContext(req, rec)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/markdown")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `.md"`)

	body := rec.Body.String()
	assert.Contains(t, body, "# 文章导出")
	assert.Contains(t, body, "文章数: 2")
	assert.Contains(t, body, "## 3. 芯片量产")
	assert.Contains(t, body, "分类: 科技/AI")
	assert.Contains(t, body, "状态: published")
	assert.Contains(t, body, "来源: xinhua.net (rss)")
	assert.Contains(t, body, "标签: 芯片")
	assert.Contains(t, body, "可信度: 0.90")
	assert.Contains(t, body, `\- 看似列表的摘要`) // heading/list markers escaped
	assert.Contains(t, body, "正文内容")
	// Second article: content fallback, no empty summary block.
	assert.Contains(t, body, "## 4. 第二条")
	assert.Contains(t, body, "fallback body")
	assert.NotContains(t, body, "**摘要**\n\n\n")
}

// ---------------------------------------------------------------------------
// ExportArticles — JSON
// ---------------------------------------------------------------------------

func TestExportArticles_JSON(t *testing.T) {
	pub := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	rows := []models.ArticleRow{
		{ID: 7, Title: "标题", Category: models.CategoryTechAI, Status: "published",
			Summary: "s", SourceURL: "https://example.com/x", Tags: []string{"t"}, FetchedAt: pub, PublishedAt: &pub},
	}
	m := &mockStore{}
	m.listFn = pageOf(rows)
	h := newHandler(m, nil, nil)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/articles/export?format=json", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.ExportArticles(e.NewContext(req, rec)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `attachment; filename="articles-`)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `.json"`)

	var got []models.ArticleRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, int64(7), got[0].ID)
	assert.Equal(t, "标题", got[0].Title)
}

// ---------------------------------------------------------------------------
// ExportArticles — pagination, filters, errors
// ---------------------------------------------------------------------------

func TestExportArticles_PaginatesBeyondOnePage(t *testing.T) {
	// 250 rows → three internal pages of 100.
	rows := make([]models.ArticleRow, 250)
	for i := range rows {
		rows[i] = models.ArticleRow{ID: int64(i + 1), Title: fmt.Sprintf("t%d", i+1)}
	}
	m := &mockStore{}
	m.listFn = pageOf(rows)
	h := newHandler(m, nil, nil)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/articles/export", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.ExportArticles(e.NewContext(req, rec)))
	assert.Equal(t, http.StatusOK, rec.Code)

	body := strings.TrimPrefix(rec.Body.String(), "\uFEFF")
	count := strings.Count(body, "\n") - 1 // minus header
	assert.Equal(t, 250, count)
}

func TestExportArticles_FiltersForwardedToStore(t *testing.T) {
	var seen models.ArticleFilter
	m := &mockStore{}
	m.listFn = func(f models.ArticleFilter) ([]models.ArticleRow, int, error) {
		if f.Page == 1 {
			seen = f
		}
		return nil, 0, nil
	}
	h := newHandler(m, nil, nil)
	e := newEcho()

	q := url.Values{}
	q.Set("category", "金融")
	q.Set("status", "published")
	q.Set("date_from", "2026-08-01")
	q.Set("date_to", "2026-08-02")
	q.Set("q", "利率")
	q.Set("page", "5")      // client pagination must be ignored
	q.Set("page_size", "7") // ditto
	req := httptest.NewRequest(http.MethodGet, "/api/articles/export?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.ExportArticles(e.NewContext(req, rec)))

	require.NotNil(t, seen.Category)
	assert.Equal(t, models.CategoryFinance, *seen.Category)
	require.NotNil(t, seen.Status)
	assert.Equal(t, "published", *seen.Status)
	require.NotNil(t, seen.DateFrom)
	assert.Equal(t, "2026-08-01", seen.DateFrom.Format(time.DateOnly))
	require.NotNil(t, seen.DateTo)
	assert.Equal(t, "2026-08-02T23:59:59Z", seen.DateTo.UTC().Format("2006-01-02T15:04:05Z"))
	assert.Equal(t, "利率", seen.Query)
	// Internal paging always starts at page 1 with the export page size.
	assert.Equal(t, 1, seen.Page)
	assert.Equal(t, 100, seen.PageSize)
}

func TestExportArticles_RowCapExceeded(t *testing.T) {
	rows := make([]models.ArticleRow, 10100)
	for i := range rows {
		rows[i] = models.ArticleRow{ID: int64(i + 1)}
	}
	m := &mockStore{}
	m.listFn = pageOf(rows)
	h := newHandler(m, nil, nil)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/articles/export", nil)
	rec := httptest.NewRecorder()
	err := h.ExportArticles(e.NewContext(req, rec))
	require.NoError(t, err) // error is rendered as JSON, not returned
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "narrow the filters")
}

func TestExportArticles_InvalidFormat(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/articles/export?format=xml", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.ExportArticles(e.NewContext(req, rec)))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "csv, json or markdown")
}

func TestExportArticles_InvalidDate(t *testing.T) {
	h := newHandler(&mockStore{}, nil, nil)
	e := newEcho()

	req := httptest.NewRequest(http.MethodGet, "/api/articles/export?date_from=not-a-date", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h.ExportArticles(e.NewContext(req, rec)))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "date_from")
}

func TestExportArticles_NilStore_503(t *testing.T) {
	h := newHandler(nil, nil, nil)
	e := newEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/articles/export", nil)
	rec := httptest.NewRecorder()
	err := h.ExportArticles(e.NewContext(req, rec))
	require.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestExportArticles_RouteRegistered(t *testing.T) {
	// "/articles/export" is registered before the "/articles/:id" family; the
	// static segment must win over the parameterised one.
	e := echo.New()
	g := e.Group("/api")
	m := &mockStore{}
	m.listFn = pageOf(nil)
	h := newHandler(m, nil, nil)
	h.Register(g)

	req := httptest.NewRequest(http.MethodGet, "/api/articles/export", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}
