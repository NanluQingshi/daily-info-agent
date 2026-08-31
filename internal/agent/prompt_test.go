package agent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// langCapturingHandler records the system message of every LLM request so
// tests can assert which reply-language prompt the agent sent.
func langCapturingHandler(mu *sync.Mutex, systemContents *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &payload)
		mu.Lock()
		if len(payload.Messages) > 0 {
			*systemContents = append(*systemContents, payload.Messages[0].Content)
		}
		mu.Unlock()
		writeJSON(w, stopResp("ok"))
	}
}

// TestRunWithLang_OverridesSystemPrompt verifies that a per-request language
// reaches the LLM as the session's system message, and that a later turn
// without an explicit lang keeps the switched language.
func TestRunWithLang_OverridesSystemPrompt(t *testing.T) {
	var mu sync.Mutex
	var systemContents []string

	r := newRunner(t, langCapturingHandler(&mu, &systemContents))

	ctx := context.Background()

	// Turn 1: explicit English on a new session.
	res1, err := r.RunWithLang(ctx, "", "hello", "en")
	require.NoError(t, err)
	require.NotEmpty(t, res1.SessionID)

	// Turn 2: same session, no explicit lang — should keep English.
	_, err = r.Run(ctx, res1.SessionID, "again")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, systemContents, 2, "one system message per turn")
	assert.Contains(t, systemContents[0], "Reply in English")
	assert.Contains(t, systemContents[1], "Reply in English")
}

// TestRunWithLang_SwitchesExistingSession verifies the language can be
// switched mid-session.
func TestRunWithLang_SwitchesExistingSession(t *testing.T) {
	var mu sync.Mutex
	var systemContents []string

	r := newRunner(t, langCapturingHandler(&mu, &systemContents))

	ctx := context.Background()
	res1, err := r.Run(ctx, "", "你好")
	require.NoError(t, err)

	_, err = r.RunWithLang(ctx, res1.SessionID, "switch", "auto")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, systemContents, 2)
	assert.Contains(t, systemContents[0], "回复使用中文")
	assert.Contains(t, systemContents[1], "same language the user writes in")
}

// TestRunWithLang_InvalidLangFallsBackToZh verifies an unrecognised request
// language degrades to Chinese rather than erroring.
func TestRunWithLang_InvalidLangFallsBackToZh(t *testing.T) {
	var mu sync.Mutex
	var systemContents []string

	r := newRunner(t, langCapturingHandler(&mu, &systemContents))

	_, err := r.RunWithLang(context.Background(), "", "bonjour", "fr")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, systemContents, 1)
	assert.Contains(t, systemContents[0], "回复使用中文")
}

// TestWithReplyLang_Default verifies the builder-level default reaches the
// system message when the request carries no language.
func TestWithReplyLang_Default(t *testing.T) {
	var mu sync.Mutex
	var systemContents []string

	r := newRunner(t, langCapturingHandler(&mu, &systemContents)).WithReplyLang("auto")

	_, err := r.Run(context.Background(), "", "hi")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, systemContents, 1)
	assert.Contains(t, systemContents[0], "same language the user writes in")
}
