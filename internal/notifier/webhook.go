package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/user/daily-info-agent/pkg/models"
)

// Webhook kinds supported by WebhookSender.
const (
	KindTelegram = "telegram"
	KindWeCom    = "wecom"
	KindDingTalk = "dingtalk"
)

// digestMaxPerCategory caps articles per category in IM digests; IM messages
// must stay short (Telegram and WeCom markdown both cap around 4096 chars).
const digestMaxPerCategory = 3

// WebhookConfig describes one IM webhook channel.
type WebhookConfig struct {
	Kind   string // KindTelegram | KindWeCom | KindDingTalk
	Token  string // telegram: bot token; dingtalk: access_token; wecom: unused (URL carries the key)
	Chat   string // telegram: chat id
	URL    string // wecom: full webhook URL; when set, overrides token-derived URLs too
	Secret string // dingtalk: optional HMAC secret for signed robots
}

// WebhookSender posts digests and alerts to an IM webhook.
type WebhookSender struct {
	cfg    WebhookConfig
	client *http.Client
	logger *slog.Logger
}

// NewWebhookSender creates a sender for the given channel config. A nil logger
// is tolerated (replaced with the default logger) to keep construction simple.
func NewWebhookSender(cfg WebhookConfig, logger *slog.Logger) *WebhookSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookSender{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		logger: logger,
	}
}

// Name identifies the channel in logs.
func (w *WebhookSender) Name() string { return w.cfg.Kind }

// SendDailySummary posts a compact markdown digest of the run.
func (w *WebhookSender) SendDailySummary(ctx context.Context, articles []models.ProcessedArticle, result models.RunResult) error {
	return w.postMessage(ctx, renderDigestText(articles, result, digestMaxPerCategory))
}

// SendAlert posts a short alert message.
func (w *WebhookSender) SendAlert(ctx context.Context, message string) error {
	return w.postMessage(ctx, message)
}

// postMessage delivers text via the channel-appropriate endpoint and payload.
func (w *WebhookSender) postMessage(ctx context.Context, text string) error {
	endpoint, body, err := w.buildRequest(text)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer resp.Body.Close()

	// IM APIs commonly wrap failures in HTTP 200 bodies; decode and check both.
	var wr struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&wr)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s: HTTP %d", w.cfg.Kind, resp.StatusCode)
	}
	if wr.ErrCode != 0 {
		return fmt.Errorf("webhook %s: errcode=%d %s", w.cfg.Kind, wr.ErrCode, wr.ErrMsg)
	}
	if w.cfg.Kind == KindTelegram && !wr.OK && wr.Description != "" {
		return fmt.Errorf("webhook telegram: %s", wr.Description)
	}

	w.logger.Debug("webhook delivered",
		slog.String("kind", w.cfg.Kind),
		slog.Int("status", resp.StatusCode),
	)
	return nil
}

// buildRequest returns the endpoint URL and JSON body for the channel kind.
func (w *WebhookSender) buildRequest(text string) (string, []byte, error) {
	switch w.cfg.Kind {
	case KindTelegram:
		endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", w.cfg.Token)
		if w.cfg.URL != "" {
			endpoint = w.cfg.URL
		}
		body, err := json.Marshal(map[string]string{
			"chat_id": w.cfg.Chat,
			"text":    text,
		})
		return endpoint, body, err

	case KindWeCom:
		if w.cfg.URL == "" {
			return "", nil, fmt.Errorf("wecom webhook requires URL")
		}
		body, err := json.Marshal(map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]string{"content": text},
		})
		return w.cfg.URL, body, err

	case KindDingTalk:
		endpoint := fmt.Sprintf("https://oapi.dingtalk.com/robot/send?access_token=%s", w.cfg.Token)
		if w.cfg.URL != "" {
			endpoint = w.cfg.URL
		}
		if w.cfg.Secret != "" {
			ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
			sign, err := dingtalkSign(ts, w.cfg.Secret)
			if err != nil {
				return "", nil, fmt.Errorf("dingtalk sign: %w", err)
			}
			sep := "&"
			if !strings.Contains(endpoint, "?") {
				sep = "?"
			}
			endpoint += fmt.Sprintf("%stimestamp=%s&sign=%s", sep, ts, url.QueryEscape(sign))
		}
		body, err := json.Marshal(map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]string{"title": "每日资讯摘要", "text": text},
		})
		return endpoint, body, err

	default:
		return "", nil, fmt.Errorf("unknown webhook kind %q", w.cfg.Kind)
	}
}

// dingtalkSign computes the signature secret-protected robots require:
// base64(hmac_sha256(key=secret, msg=timestamp+"\n"+secret)).
func dingtalkSign(timestamp, secret string) (string, error) {
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(timestamp + "\n" + secret)); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// renderDigestText builds the compact plain-text/markdown digest shared by all
// IM channels: date header, run stats, then up to maxPerCat top-credibility
// articles per category. Capped to stay under IM length limits.
func renderDigestText(articles []models.ProcessedArticle, result models.RunResult, maxPerCat int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📰 每日资讯摘要 %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "抓取 %d · 处理 %d · 发布 %d · 跳过 %d\n",
		result.TotalFetched, result.TotalProcessed, result.TotalPublished, result.TotalSkipped)

	byCategory := make(map[models.Category][]models.ProcessedArticle)
	for _, a := range articles {
		byCategory[a.Category] = append(byCategory[a.Category], a)
	}

	for _, cat := range models.AllCategories {
		arts := byCategory[cat]
		if len(arts) == 0 {
			continue
		}
		sort.Slice(arts, func(i, j int) bool { return arts[i].CredibilityScore > arts[j].CredibilityScore })
		if len(arts) > maxPerCat {
			arts = arts[:maxPerCat]
		}
		fmt.Fprintf(&b, "\n**%s**\n", cat)
		for _, a := range arts {
			fmt.Fprintf(&b, "- [%s](%s)（%s · 可信度 %d%%）\n",
				strings.ReplaceAll(a.Raw.Title, "\n", " "),
				a.Raw.URL,
				a.Raw.SourceDomain,
				int(a.CredibilityScore*100),
			)
		}
	}

	// Hard ceiling for IM length limits (Telegram/WeCom ≈ 4096).
	const maxRunes = 3800
	runes := []rune(b.String())
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "\n…（更多内容见网站）"
	}
	return b.String()
}
