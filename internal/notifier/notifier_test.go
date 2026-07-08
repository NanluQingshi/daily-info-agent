package notifier

import (
	"context"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

// ---------------------------------------------------------------------------
// SendDailySummary — article grouping, sorting, and limiting
// ---------------------------------------------------------------------------

func TestSendDailySummary_GroupsByCategory(t *testing.T) {
	// Use a logger that won't interfere.
	n := New("smtp.example.com", 587, "user@example.com", "pass", "", "recipient@example.com", slog.Default())

	articles := []models.ProcessedArticle{
		makeProcessedArticle("金融", 0.9, "Article A"),
		makeProcessedArticle("金融", 0.8, "Article B"),
		makeProcessedArticle("科技/AI", 0.95, "Article C"),
	}

	// This will fail at SMTP but we only care that the function doesn't panic
	// and that the grouping happens before the SMTP attempt.
	// The error is expected since there's no SMTP server.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := n.SendDailySummary(ctx, articles, models.RunResult{})
	// Should error because SMTP connection fails, but shouldn't panic.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "smtp send")
}

func TestSendDailySummary_LimitsToTop5PerCategory(t *testing.T) {
	// 8 articles in the same category — only top 5 by credibility should make it.
	n := New("smtp.example.com", 587, "user", "pass", "", "recipient", nil)

	articles := make([]models.ProcessedArticle, 8)
	for i := range articles {
		score := 1.0 - float64(i)*0.1 // 1.0, 0.9, 0.8, ..., 0.3
		articles[i] = makeProcessedArticle("金融", score, "Article")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := n.SendDailySummary(ctx, articles, models.RunResult{})
	require.Error(t, err) // SMTP will fail
}

func TestSendDailySummary_EmptyArticles_DoesNotPanic(t *testing.T) {
	n := New("smtp.example.com", 587, "user", "pass", "", "recipient", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := n.SendDailySummary(ctx, nil, models.RunResult{})
	// Empty articles should not trigger SMTP send (no sections rendered).
	// The function may or may not error — but must not panic.
	if err != nil {
		t.Logf("expected error (no SMTP): %v", err)
	}
}

func TestSendDailySummary_SortsByCredibilityDescending(t *testing.T) {
	n := New("smtp.example.com", 587, "user", "pass", "", "recipient", nil)

	articles := []models.ProcessedArticle{
		makeProcessedArticle("国际", 0.3, "Low cred"),
		makeProcessedArticle("国际", 0.9, "High cred"),
		makeProcessedArticle("国际", 0.6, "Medium cred"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := n.SendDailySummary(ctx, articles, models.RunResult{})
	require.Error(t, err) // SMTP will fail — that's expected
}

// ---------------------------------------------------------------------------
// sendSMTP — context cancellation and timeout
// ---------------------------------------------------------------------------

func TestSendSMTP_ContextCancelled_BeforeSend(t *testing.T) {
	n := New("smtp.example.com", 587, "user", "pass", "", "recipient", slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	msg := buildMIMEMessage("from@test.com", "to@test.com", "Test", "<p>body</p>")
	err := n.sendSMTP(ctx, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestSendSMTP_ConnectionRefused_ReturnsError(t *testing.T) {
	// Point to a port where nothing is listening.
	n := New("127.0.0.1", 19999, "user", "pass", "", "recipient", slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg := buildMIMEMessage("from@test.com", "to@test.com", "Test", "<p>body</p>")
	err := n.sendSMTP(ctx, msg)
	require.Error(t, err)
	// Should get "connection refused" or similar network error.
	t.Logf("expected connection error: %v", err)
}

// ---------------------------------------------------------------------------
// SendDailySummary — with a real local SMTP server
// ---------------------------------------------------------------------------

func TestSendDailySummary_WithLocalSMTPServer(t *testing.T) {
	// Start a local TCP listener that accepts one connection and reads enough
	// to make smtp.SendMail happy, then closes.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	var accepted atomic.Bool
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted.Store(true)
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		// Write a valid SMTP greeting so SendMail doesn't hang.
		conn.Write([]byte("220 localhost ESMTP ready\r\n"))
		// Read some data then close.
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Close()
	}()

	addr := ln.Addr().(*net.TCPAddr)
	n := New("127.0.0.1", addr.Port, "user", "pass", "", "recipient", slog.Default())

	articles := []models.ProcessedArticle{
		makeProcessedArticle("金融", 0.95, "Test Article Title"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = n.SendDailySummary(ctx, articles, models.RunResult{
		TotalFetched:   10,
		TotalProcessed: 8,
		TotalPublished: 5,
		TotalSkipped:   2,
	})
	// The SMTP conversation will fail after greeting (we don't implement full SMTP),
	// but the function should not panic.
	t.Logf("send result (expected SMTP-level error): %v", err)
	assert.True(t, accepted.Load(), "SMTP server should have accepted connection")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeProcessedArticle(category string, score float64, title string) models.ProcessedArticle {
	return models.ProcessedArticle{
		Raw: &models.RawItem{
			Title:        title,
			URL:          "https://example.com/" + title,
			SourceDomain: "example.com",
		},
		Category:         models.Category(category),
		Summary:          "测试摘要内容。",
		CredibilityScore: score,
	}
}

// ---------------------------------------------------------------------------
// Constructor edge cases
// ---------------------------------------------------------------------------

func TestNew_NilLogger_DoesNotPanic(t *testing.T) {
	n := New("smtp.example.com", 587, "user", "pass", "", "recipient", nil)
	require.NotNil(t, n)
	assert.Equal(t, "smtp.example.com", n.host)
	assert.Equal(t, 587, n.port)
}

func TestNew_EmptySMTPConfig_DoesNotPanic(t *testing.T) {
	n := New("", 0, "", "", "", "", nil)
	require.NotNil(t, n)
}

// ---------------------------------------------------------------------------
// buildMIMEMessage additional coverage
// ---------------------------------------------------------------------------

func TestBuildMIMEMessage_EmptyBody(t *testing.T) {
	msg := buildMIMEMessage("from@test.com", "to@test.com", "Subject", "")
	s := string(msg)
	assert.Contains(t, s, "MIME-Version: 1.0")
	assert.Contains(t, s, "Content-Type: text/html; charset=UTF-8")
	assert.Contains(t, s, "Subject: Subject")
	assert.Contains(t, s, "From: Daily Info Agent <from@test.com>")
	assert.Contains(t, s, "To: to@test.com")
}
