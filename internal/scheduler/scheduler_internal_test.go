package scheduler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/daily-info-agent/pkg/config"
	"github.com/user/daily-info-agent/pkg/models"
)

func TestCategoryToNewsAPIQuery_AllCategories(t *testing.T) {
	tests := []struct {
		cat  models.Category
		want string
	}{
		{models.CategoryFinance, "finance stock market"},
		{models.CategoryPolitics, "politics government policy"},
		{models.CategoryEconomy, "economy GDP trade"},
		{models.CategoryTechAI, "technology AI artificial intelligence"},
		{models.CategoryInternational, "international world news"},
	}
	for _, tc := range tests {
		t.Run(string(tc.cat), func(t *testing.T) {
			assert.Equal(t, tc.want, categoryToNewsAPIQuery(tc.cat))
		})
	}
}

func TestCategoryToNewsAPIQuery_UnknownCategory(t *testing.T) {
	assert.Equal(t, "未知", categoryToNewsAPIQuery("未知"))
}

func TestCategoryToSearchQuery_AllCategories(t *testing.T) {
	tests := []struct {
		cat  models.Category
		want string
	}{
		{models.CategoryFinance, "finance stock market economy news today"},
		{models.CategoryPolitics, "politics government policy news today"},
		{models.CategoryEconomy, "economy GDP trade business news today"},
		{models.CategoryTechAI, "technology AI artificial intelligence news today"},
		{models.CategoryInternational, "world international breaking news today"},
	}
	for _, tc := range tests {
		t.Run(string(tc.cat), func(t *testing.T) {
			assert.Equal(t, tc.want, categoryToSearchQuery(tc.cat))
		})
	}
}

func TestCategoryToSearchQuery_UnknownCategory(t *testing.T) {
	q := categoryToSearchQuery("体育")
	assert.Contains(t, q, "体育")
	assert.Contains(t, q, "latest news")
}

func TestBuildFetchConfigs_EmptyCategories(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{}}
	cfgs := s.buildFetchConfigs(nil)
	assert.Empty(t, cfgs)
}

func TestBuildFetchConfigs_WithRSSFeeds(t *testing.T) {
	s := &Scheduler{
		cfg: &config.Config{
			RSSFeeds: []string{"https://feeds.example.com/rss"},
		},
	}
	cfgs := s.buildFetchConfigs([]models.Category{models.CategoryFinance})
	assert.NotEmpty(t, cfgs)
}

func TestBuildFetchConfigs_WithSearchEngine(t *testing.T) {
	s := &Scheduler{
		cfg: &config.Config{
			SearchEngineEnabled: true,
		},
	}
	cfgs := s.buildFetchConfigs([]models.Category{models.CategoryTechAI})
	assert.NotEmpty(t, cfgs)
}
