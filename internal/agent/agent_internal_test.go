package agent

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/daily-info-agent/pkg/models"
)

func TestFormatItems_Empty(t *testing.T) {
	result := formatItems(nil)
	assert.Contains(t, result, "0 篇")
}

func TestFormatItems_SingleItem(t *testing.T) {
	items := []models.RawItem{
		{Title: "Test Article", URL: "https://example.com/article", SourceDomain: "example.com", Description: "A test article"},
	}
	result := formatItems(items)
	assert.Contains(t, result, "1 篇")
	assert.Contains(t, result, "Test Article")
	assert.Contains(t, result, "example.com")
}

func TestFormatItems_MultipleItems(t *testing.T) {
	items := []models.RawItem{
		{Title: "Article 1", URL: "https://example.com/1", SourceDomain: "source1.com", Description: "Desc 1"},
		{Title: "Article 2", URL: "https://example.com/2", SourceDomain: "source2.com", Description: "Desc 2"},
	}
	result := formatItems(items)
	assert.Contains(t, result, "2 篇")
	assert.Contains(t, result, "Article 1")
	assert.Contains(t, result, "Article 2")
}

func TestTruncateForLog_ShortEnough(t *testing.T) {
	s := "hello world"
	assert.Equal(t, s, truncateForLog(s, 100))
}

func TestTruncateForLog_ExactLength(t *testing.T) {
	s := "hello"
	assert.Equal(t, s, truncateForLog(s, 5))
}

func TestTruncateForLog_Truncated(t *testing.T) {
	s := "hello world this is a test"
	result := truncateForLog(s, 5)
	assert.Equal(t, "hello…", result)
}

func TestTruncateForLog_MultibyteSafe(t *testing.T) {
	s := "你好世界"
	result := truncateForLog(s, 2)
	assert.Equal(t, "你好…", result)
}

func TestTruncateForLog_Empty(t *testing.T) {
	assert.Equal(t, "", truncateForLog("", 10))
}

func TestCategoryTopicKeyword_AllCategories(t *testing.T) {
	tests := []struct {
		cat  models.Category
		want string
		ok   bool
	}{
		{models.CategoryFinance, "finance", true},
		{models.CategoryPolitics, "politics", true},
		{models.CategoryEconomy, "economy", true},
		{models.CategoryTechAI, "technology AI", true},
		{models.CategoryInternational, "world international", true},
	}
	for _, tc := range tests {
		t.Run(string(tc.cat), func(t *testing.T) {
			got, ok := categoryTopicKeyword(string(tc.cat))
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCategoryTopicKeyword_Unknown(t *testing.T) {
	got, ok := categoryTopicKeyword("未知")
	assert.False(t, ok)
	assert.Empty(t, got)
}

func TestCategoryTopicKeyword_Empty(t *testing.T) {
	got, ok := categoryTopicKeyword("")
	assert.False(t, ok)
	assert.Empty(t, got)
}

func TestDeleteSession_NoopForUnknown(t *testing.T) {
	r := &Runner{sessions: NewSessionStore(nil, slog.Default())}
	// Should not panic for unknown session
	r.DeleteSession("nonexistent")
}

func TestSessionDelete_RemovesEntry(t *testing.T) {
	s := NewSessionStore(nil, slog.Default())
	s.Set("test-id", nil)
	s.Delete("test-id")
	assert.Nil(t, s.Get("test-id"))
}

func TestSessionDelete_NoopForUnknown(t *testing.T) {
	s := NewSessionStore(nil, slog.Default())
	// Should not panic
	s.Delete("nonexistent")
}
