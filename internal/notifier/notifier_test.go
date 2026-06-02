package notifier

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/daily-info-agent/pkg/models"
)

func TestBuildMIMEMessage(t *testing.T) {
	msg := buildMIMEMessage("from@example.com", "to@example.com", "Test Subject", "<p>body</p>")
	s := string(msg)
	assert.Contains(t, s, "Content-Type: text/html; charset=UTF-8")
	assert.Contains(t, s, "Subject: Test Subject")
	assert.Contains(t, s, "<p>body</p>")
}

func TestTemplateRenders(t *testing.T) {
	articles := []articleEntry{
		{
			Title:          "测试文章标题",
			URL:            "https://reuters.com/test",
			SourceDomain:   "reuters.com",
			Summary:        "这是一篇测试文章的摘要内容。",
			CredibilityPct: 85,
		},
	}
	result := models.RunResult{
		TotalFetched:   10,
		TotalProcessed: 8,
		TotalPublished: 5,
		TotalSkipped:   3,
	}
	data := summaryData{
		Date: "2026-06-02",
		Categories: []categorySection{
			{Name: string(models.CategoryFinance), Articles: articles},
		},
		Result: result,
	}

	var buf bytes.Buffer
	err := emailTemplate.Execute(&buf, data)
	require.NoError(t, err)

	body := buf.String()
	assert.Contains(t, body, "测试文章标题")
	assert.Contains(t, body, "reuters.com")
	assert.Contains(t, body, "85%")
	assert.Contains(t, body, "金融")
}

func TestNewDefaultsFrom(t *testing.T) {
	n := New("smtp.example.com", 587, "user@example.com", "pass", "", "recipient@example.com", nil)
	assert.Equal(t, "user@example.com", n.from)
}

func TestNewExplicitFrom(t *testing.T) {
	n := New("smtp.example.com", 587, "user@example.com", "pass", "agent@example.com", "recipient@example.com", nil)
	assert.Equal(t, "agent@example.com", n.from)
}

