package scheduler

import (
	"context"
	"errors"
	"log/slog"
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
	cfgs := s.buildFetchConfigs(context.Background(), nil)
	assert.Empty(t, cfgs)
}

func TestBuildFetchConfigs_WithRSSFeeds(t *testing.T) {
	s := &Scheduler{
		cfg: &config.Config{
			RSSFeeds: []string{"https://feeds.example.com/rss"},
		},
	}
	cfgs := s.buildFetchConfigs(context.Background(), []models.Category{models.CategoryFinance})
	assert.NotEmpty(t, cfgs)
}

func TestBuildFetchConfigs_WithSearchEngine(t *testing.T) {
	s := &Scheduler{
		cfg: &config.Config{
			SearchEngineEnabled: true,
		},
	}
	cfgs := s.buildFetchConfigs(context.Background(), []models.Category{models.CategoryTechAI})
	assert.NotEmpty(t, cfgs)
}

func TestResolveSourceURLs_ProviderPrecedence(t *testing.T) {
	staticList := []string{"https://static.example/rss"}

	t.Run("provider unset falls back to config", func(t *testing.T) {
		s := &Scheduler{cfg: &config.Config{RSSFeeds: staticList}, logger: slog.Default()}
		assert.Equal(t, staticList, s.resolveSourceURLs(context.Background()))
	})

	t.Run("provider rows win", func(t *testing.T) {
		s := &Scheduler{cfg: &config.Config{RSSFeeds: staticList}, logger: slog.Default()}
		s.WithSourcesProvider(func(context.Context) ([]string, error) {
			return []string{"https://managed.example/rss"}, nil
		})
		assert.Equal(t, []string{"https://managed.example/rss"}, s.resolveSourceURLs(context.Background()))
	})

	t.Run("nil result falls back to config", func(t *testing.T) {
		s := &Scheduler{cfg: &config.Config{RSSFeeds: staticList}, logger: slog.Default()}
		s.WithSourcesProvider(func(context.Context) ([]string, error) { return nil, nil })
		assert.Equal(t, staticList, s.resolveSourceURLs(context.Background()))
	})

	t.Run("empty non-nil result is respected (all disabled)", func(t *testing.T) {
		s := &Scheduler{cfg: &config.Config{RSSFeeds: staticList}, logger: slog.Default()}
		s.WithSourcesProvider(func(context.Context) ([]string, error) { return []string{}, nil })
		assert.Empty(t, s.resolveSourceURLs(context.Background()))
	})

	t.Run("provider error falls back to config", func(t *testing.T) {
		s := &Scheduler{cfg: &config.Config{RSSFeeds: staticList}, logger: slog.Default()}
		s.WithSourcesProvider(func(context.Context) ([]string, error) { return nil, errors.New("db down") })
		assert.Equal(t, staticList, s.resolveSourceURLs(context.Background()))
	})
}
