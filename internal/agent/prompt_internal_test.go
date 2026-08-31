package agent

import (
	"strings"
	"testing"
)

func TestSystemPromptFor_Zh(t *testing.T) {
	p := systemPromptFor("zh")
	if !strings.Contains(p, "回复使用中文，简洁清晰") {
		t.Errorf("zh prompt should carry the Chinese reply rule")
	}
}

func TestSystemPromptFor_En(t *testing.T) {
	p := systemPromptFor("en")
	if !strings.Contains(p, "Reply in English") {
		t.Errorf("en prompt should carry the English reply rule")
	}
	if strings.Contains(p, "回复使用中文") {
		t.Errorf("en prompt must not carry the Chinese reply rule")
	}
}

func TestSystemPromptFor_Auto(t *testing.T) {
	p := systemPromptFor("auto")
	if !strings.Contains(p, "same language the user writes in") {
		t.Errorf("auto prompt should tell the model to mirror the user's language")
	}
}

func TestSystemPromptFor_UnknownFallsBackToZh(t *testing.T) {
	if systemPromptFor("fr") != systemPromptFor("zh") {
		t.Errorf("unknown lang should fall back to zh prompt")
	}
	if systemPromptFor("") != systemPromptFor("zh") {
		t.Errorf("empty lang should fall back to zh prompt")
	}
}

func TestSystemPromptFor_SharedBody(t *testing.T) {
	// The tool-guidance body must be present in every mode; only the
	// reply-language bullet differs.
	for _, lang := range []string{"zh", "en", "auto"} {
		p := systemPromptFor(lang)
		if !strings.Contains(p, "search_news") || !strings.Contains(p, "search_stored_articles") {
			t.Errorf("prompt for %q lost its tool guidance", lang)
		}
		if !strings.Contains(p, "get_current_time") {
			t.Errorf("prompt for %q lost the time-tool guidance", lang)
		}
	}
}
