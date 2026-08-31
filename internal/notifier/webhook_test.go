package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/user/daily-info-agent/pkg/models"
)

// captured records one webhook request for assertions.
type captured struct {
	path string // URL path (no query)
	body map[string]any
}

// captureServer starts an httptest server that records every request body.
func captureServer(t *testing.T, status int, respBody string) (*captured, *httptest.Server) {
	t.Helper()
	cap := &captured{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		cap.path = r.URL.Path
		cap.body = body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return cap, srv
}

func testArticles() []models.ProcessedArticle {
	return []models.ProcessedArticle{
		{
			Category:         models.CategoryTechAI,
			Summary:          "测试摘要一",
			CredibilityScore: 0.9,
			Raw:              &models.RawItem{Title: "文章一", URL: "https://a.example/1", SourceDomain: "a.example"},
		},
		{
			Category:         models.CategoryTechAI,
			Summary:          "测试摘要二",
			CredibilityScore: 0.7,
			Raw:              &models.RawItem{Title: "文章二", URL: "https://a.example/2", SourceDomain: "a.example"},
		},
	}
}

func TestWebhookSender_TelegramDigest(t *testing.T) {
	cap, srv := captureServer(t, 200, `{"ok":true}`)
	s := NewWebhookSender(WebhookConfig{
		Kind: KindTelegram, Token: "bot123", Chat: "42", URL: srv.URL + "/bot123/sendMessage",
	}, nil)

	if err := s.SendDailySummary(context.Background(), testArticles(), models.RunResult{TotalFetched: 2, TotalProcessed: 2}); err != nil {
		t.Fatalf("SendDailySummary: %v", err)
	}

	if !strings.HasSuffix(cap.path, "/bot123/sendMessage") {
		t.Errorf("telegram path = %q, want /bot123/sendMessage", cap.path)
	}
	if cap.body["chat_id"] != "42" {
		t.Errorf("chat_id = %v, want 42", cap.body["chat_id"])
	}
	text, _ := cap.body["text"].(string)
	if !strings.Contains(text, "文章一") || !strings.Contains(text, "https://a.example/1") {
		t.Errorf("digest text missing article: %q", text)
	}
	if !strings.Contains(text, "抓取 2") {
		t.Errorf("digest text missing stats: %q", text)
	}
}

func TestWebhookSender_TelegramAPIError(t *testing.T) {
	_, srv := captureServer(t, 200, `{"ok":false,"description":"Bad Request: chat not found"}`)
	s := NewWebhookSender(WebhookConfig{
		Kind: KindTelegram, Token: "b", Chat: "1", URL: srv.URL + "/sendMessage",
	}, nil)

	err := s.SendAlert(context.Background(), "alert")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("expected telegram API error, got %v", err)
	}
}

func TestWebhookSender_WeComPayload(t *testing.T) {
	cap, srv := captureServer(t, 200, `{"errcode":0}`)
	s := NewWebhookSender(WebhookConfig{Kind: KindWeCom, URL: srv.URL}, nil)

	if err := s.SendAlert(context.Background(), "⚠️ 告警"); err != nil {
		t.Fatalf("SendAlert: %v", err)
	}

	if cap.body["msgtype"] != "markdown" {
		t.Errorf("msgtype = %v, want markdown", cap.body["msgtype"])
	}
	md, _ := cap.body["markdown"].(map[string]any)
	if md["content"] != "⚠️ 告警" {
		t.Errorf("markdown.content = %v", md["content"])
	}
}

func TestWebhookSender_WeComError(t *testing.T) {
	_, srv := captureServer(t, 200, `{"errcode":93000,"errmsg":"invalid webhook url"}`)
	s := NewWebhookSender(WebhookConfig{Kind: KindWeCom, URL: srv.URL}, nil)

	err := s.SendAlert(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "93000") {
		t.Errorf("expected wecom errcode error, got %v", err)
	}
}

func TestWebhookSender_DingTalkSign(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()

	s := NewWebhookSender(WebhookConfig{
		Kind: KindDingTalk, Token: "tok", Secret: "sec", URL: srv.URL,
	}, nil)

	if err := s.SendAlert(context.Background(), "alert"); err != nil {
		t.Fatalf("SendAlert: %v", err)
	}

	if !strings.Contains(gotQuery, "timestamp=") || !strings.Contains(gotQuery, "sign=") {
		t.Errorf("signed robot missing timestamp/sign: %q", gotQuery)
	}
	// Sign must round-trip through the documented HMAC construction.
	q := splitQuery(gotQuery)
	if sign, ok := q["sign"]; ok {
		ts := q["timestamp"]
		want, _ := dingtalkSign(ts, "sec")
		if sign != want {
			t.Errorf("sign mismatch: got %q want %q", sign, want)
		}
	}
}

func splitQuery(raw string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(raw, "&") {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			if v, err := url.QueryUnescape(parts[1]); err == nil {
				parts[1] = v
			}
			out[parts[0]] = parts[1]
		}
	}
	return out
}

func TestWebhookSender_DingTalkNoSecret_NoSign(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()

	s := NewWebhookSender(WebhookConfig{Kind: KindDingTalk, Token: "tok", URL: srv.URL}, nil)
	if err := s.SendAlert(context.Background(), "x"); err != nil {
		t.Fatalf("SendAlert: %v", err)
	}
	if strings.Contains(gotQuery, "sign=") {
		t.Errorf("unsigned robot should not carry sign: %q", gotQuery)
	}
}

func TestWebhookSender_HTTPError(t *testing.T) {
	_, srv := captureServer(t, 500, `{}`)
	s := NewWebhookSender(WebhookConfig{Kind: KindWeCom, URL: srv.URL}, nil)

	err := s.SendAlert(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected HTTP error, got %v", err)
	}
}

func TestWebhookSender_UnknownKind(t *testing.T) {
	s := NewWebhookSender(WebhookConfig{Kind: "slack"}, nil)
	err := s.SendAlert(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "unknown webhook kind") {
		t.Errorf("expected unknown kind error, got %v", err)
	}
}

func TestWebhookSender_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until client gives up
	}))
	defer srv.Close()

	s := NewWebhookSender(WebhookConfig{Kind: KindWeCom, URL: srv.URL}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.SendAlert(ctx, "x"); err == nil {
		t.Error("expected context error, got nil")
	}
}

func TestRenderDigestText_CapsPerCategory(t *testing.T) {
	arts := make([]models.ProcessedArticle, 6)
	for i := range arts {
		arts[i] = models.ProcessedArticle{
			Category:         models.CategoryTechAI,
			CredibilityScore: 0.5,
			Raw:              &models.RawItem{Title: "t", URL: "https://x/" + string(rune('a'+i)), SourceDomain: "x"},
		}
	}
	text := renderDigestText(arts, models.RunResult{}, 3)
	if got := strings.Count(text, "- [t]"); got != 3 {
		t.Errorf("expected 3 article lines, got %d in:\n%s", got, text)
	}
}

func TestRenderDigestText_Truncates(t *testing.T) {
	var arts []models.ProcessedArticle
	for _, cat := range models.AllCategories {
		for i := 0; i < 5; i++ {
			arts = append(arts, models.ProcessedArticle{
				Category:         cat,
				CredibilityScore: 0.5,
				Raw: &models.RawItem{
					Title:        strings.Repeat("很长的标题", 40),
					URL:          "https://x/" + strings.Repeat("u", 60),
					SourceDomain: "x",
				},
			})
		}
	}
	text := renderDigestText(arts, models.RunResult{}, 3)
	if got := len([]rune(text)); got > 3900 {
		t.Errorf("digest too long: %d runes", got)
	}
	if !strings.Contains(text, "更多内容见网站") {
		t.Errorf("truncated digest missing footer")
	}
}

func TestWebhookSender_BuildRequest_TokenDerivedEndpoints(t *testing.T) {
	w := NewWebhookSender(WebhookConfig{Kind: KindTelegram, Token: "T", Chat: "C"}, nil)
	ep, body, err := w.buildRequest("hi")
	if err != nil || ep != "https://api.telegram.org/botT/sendMessage" {
		t.Errorf("telegram endpoint = %q err=%v", ep, err)
	}
	if !strings.Contains(string(body), `"chat_id":"C"`) {
		t.Errorf("telegram body = %s", body)
	}

	w = NewWebhookSender(WebhookConfig{Kind: KindDingTalk, Token: "tok"}, nil)
	ep, _, err = w.buildRequest("hi")
	if err != nil || !strings.Contains(ep, "access_token=tok") {
		t.Errorf("dingtalk endpoint = %q err=%v", ep, err)
	}

	w = NewWebhookSender(WebhookConfig{Kind: KindWeCom}, nil)
	if _, _, err := w.buildRequest("hi"); err == nil {
		t.Error("wecom without URL should error")
	}
}
