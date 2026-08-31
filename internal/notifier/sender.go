package notifier

import (
	"context"

	"github.com/user/daily-info-agent/pkg/models"
)

// Sender is one notification channel. Implementations include the SMTP email
// Notifier and the webhook WebhookSender (Telegram / WeCom / DingTalk).
// All methods must be safe to call concurrently and must respect ctx
// deadlines so a hung channel cannot stall a pipeline run.
type Sender interface {
	// Name identifies the channel in logs, e.g. "email", "telegram".
	Name() string

	// SendDailySummary delivers a digest of a finished pipeline run.
	SendDailySummary(ctx context.Context, articles []models.ProcessedArticle, result models.RunResult) error

	// SendAlert delivers a short alert message (e.g. consecutive failures).
	SendAlert(ctx context.Context, message string) error
}
