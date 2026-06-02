// Package notifier sends daily email digests after a scheduled pipeline run.
package notifier

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/smtp"
	"sort"
	"time"

	"github.com/user/daily-info-agent/pkg/models"
)

// Notifier sends HTML email summaries via SMTP.
type Notifier struct {
	host     string
	port     int
	user     string
	password string
	from     string
	to       string
	logger   *slog.Logger
}

// New creates a Notifier. from defaults to user when empty.
func New(host string, port int, user, password, from, to string, logger *slog.Logger) *Notifier {
	if from == "" {
		from = user
	}
	return &Notifier{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		from:     from,
		to:       to,
		logger:   logger,
	}
}

type summaryData struct {
	Date       string
	Categories []categorySection
	Result     models.RunResult
}

type categorySection struct {
	Name     string
	Articles []articleEntry
}

type articleEntry struct {
	Title          string
	URL            string
	SourceDomain   string
	Summary        string
	CredibilityPct int // pre-computed percentage (0-100)
}

// SendDailySummary renders an HTML email with the top articles per category and sends it.
func (n *Notifier) SendDailySummary(_ context.Context, articles []models.ProcessedArticle, result models.RunResult) error {
	byCategory := make(map[models.Category][]models.ProcessedArticle)
	for _, a := range articles {
		byCategory[a.Category] = append(byCategory[a.Category], a)
	}

	var sections []categorySection
	for _, cat := range models.AllCategories {
		arts := byCategory[cat]
		if len(arts) == 0 {
			continue
		}
		sort.Slice(arts, func(i, j int) bool {
			return arts[i].CredibilityScore > arts[j].CredibilityScore
		})
		if len(arts) > 5 {
			arts = arts[:5]
		}
		entries := make([]articleEntry, len(arts))
		for i, a := range arts {
			entries[i] = articleEntry{
				Title:          a.Raw.Title,
				URL:            a.Raw.URL,
				SourceDomain:   a.Raw.SourceDomain,
				Summary:        a.Summary,
				CredibilityPct: int(a.CredibilityScore * 100),
			}
		}
		sections = append(sections, categorySection{Name: string(cat), Articles: entries})
	}

	data := summaryData{
		Date:       time.Now().Format("2006-01-02"),
		Categories: sections,
		Result:     result,
	}

	var body bytes.Buffer
	if err := emailTemplate.Execute(&body, data); err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	subject := fmt.Sprintf("每日资讯摘要 %s", data.Date)
	msg := buildMIMEMessage(n.from, n.to, subject, body.String())

	auth := smtp.PlainAuth("", n.user, n.password, n.host)
	addr := fmt.Sprintf("%s:%d", n.host, n.port)
	if err := smtp.SendMail(addr, auth, n.from, []string{n.to}, msg); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}

	n.logger.Info("daily summary email sent",
		slog.String("to", n.to),
		slog.Int("categories", len(sections)),
		slog.Int("articles", len(articles)),
	)
	return nil
}

func buildMIMEMessage(from, to, subject, htmlBody string) []byte {
	var buf bytes.Buffer
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	fmt.Fprintf(&buf, "From: Daily Info Agent <%s>\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	buf.WriteString("\r\n")
	buf.WriteString(htmlBody)
	return buf.Bytes()
}

var emailTemplate = template.Must(template.New("email").Parse(emailHTML))

const emailHTML = `<!DOCTYPE html>
<html lang="zh">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#f5f5f5;font-family:Arial,sans-serif">
<table width="100%" cellpadding="0" cellspacing="0" style="background:#f5f5f5;padding:24px 0">
<tr><td align="center">
<table width="600" cellpadding="0" cellspacing="0" style="background:#fff;border-radius:8px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,.08)">

  <!-- Header -->
  <tr><td style="background:#1a1a2e;padding:24px 32px">
    <h1 style="margin:0;color:#fff;font-size:20px;font-weight:700">每日资讯摘要</h1>
    <p style="margin:4px 0 0;color:#a0a0c0;font-size:14px">{{.Date}}</p>
  </td></tr>

  <!-- Stats bar -->
  <tr><td style="background:#16213e;padding:12px 32px">
    <table width="100%" cellpadding="0" cellspacing="0"><tr>
      <td style="color:#6c8ebf;font-size:12px;text-align:center">
        <span style="color:#fff;font-size:18px;font-weight:700">{{.Result.TotalFetched}}</span><br>抓取
      </td>
      <td style="color:#6c8ebf;font-size:12px;text-align:center">
        <span style="color:#fff;font-size:18px;font-weight:700">{{.Result.TotalProcessed}}</span><br>处理
      </td>
      <td style="color:#6c8ebf;font-size:12px;text-align:center">
        <span style="color:#4ade80;font-size:18px;font-weight:700">{{.Result.TotalPublished}}</span><br>发布
      </td>
      <td style="color:#6c8ebf;font-size:12px;text-align:center">
        <span style="color:#facc15;font-size:18px;font-weight:700">{{.Result.TotalSkipped}}</span><br>跳过
      </td>
    </tr></table>
  </td></tr>

  <!-- Categories -->
  {{range .Categories}}
  <tr><td style="padding:20px 32px 0">
    <h2 style="margin:0 0 12px;font-size:15px;color:#1a1a2e;border-left:4px solid #4f46e5;padding-left:10px">{{.Name}}</h2>
    {{range .Articles}}
    <table width="100%" cellpadding="0" cellspacing="0" style="margin-bottom:12px;border:1px solid #e8e8f0;border-radius:6px;overflow:hidden">
      <tr><td style="padding:12px 16px">
        <a href="{{.URL}}" style="color:#1a1a2e;font-size:14px;font-weight:600;text-decoration:none;line-height:1.4">{{.Title}}</a>
        <p style="margin:6px 0 0;color:#555;font-size:13px;line-height:1.6">{{.Summary}}</p>
        <p style="margin:8px 0 0;color:#999;font-size:12px">
          {{.SourceDomain}} &nbsp;·&nbsp; 可信度 {{.CredibilityPct}}%
        </p>
      </td></tr>
    </table>
    {{end}}
  </td></tr>
  {{end}}

  <!-- Footer -->
  <tr><td style="padding:20px 32px;border-top:1px solid #e8e8f0;margin-top:8px">
    <p style="margin:0;color:#aaa;font-size:12px;text-align:center">
      由 Daily Info Agent 自动生成 · {{.Date}}
    </p>
  </td></tr>

</table>
</td></tr>
</table>
</body>
</html>`
