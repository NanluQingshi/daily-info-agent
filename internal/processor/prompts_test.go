package processor

import (
	"strings"
	"testing"
)

func TestSummaryLangRule_Zh(t *testing.T) {
	got := summaryLangRule("zh")
	if !strings.Contains(got, "Chinese summary") {
		t.Errorf("zh rule should mention Chinese summary, got %q", got)
	}
}

func TestSummaryLangRule_En(t *testing.T) {
	got := summaryLangRule("en")
	if !strings.Contains(got, "English summary") {
		t.Errorf("en rule should mention English summary, got %q", got)
	}
}

func TestSummaryLangRule_Auto_FollowsArticleLanguage(t *testing.T) {
	got := summaryLangRule("auto")
	if !strings.Contains(got, "SAME language as the original article") {
		t.Errorf("auto rule should direct summary to follow the article language, got %q", got)
	}
}

func TestSummaryLangRule_UnknownFallsBackToChinese(t *testing.T) {
	if summaryLangRule("fr") != summaryLangRule("zh") {
		t.Errorf("unknown lang should fall back to zh rule")
	}
	if summaryLangRule("") != summaryLangRule("zh") {
		t.Errorf("empty lang should fall back to zh rule")
	}
}

func TestBuildBatchPrompt_SubstitutesInputAndLangRule(t *testing.T) {
	input := `[{"url":"https://example.com/a","title":"Test"}]`

	got := buildBatchPrompt(input, "en")

	if !strings.Contains(got, input) {
		t.Errorf("prompt should embed the input JSON verbatim")
	}
	if strings.Contains(got, "{{INPUT}}") || strings.Contains(got, "{{SUMMARY_RULE}}") {
		t.Errorf("prompt should have no leftover placeholders: %q", got)
	}
	if !strings.Contains(got, "English summary") {
		t.Errorf("prompt should carry the en summary directive")
	}
}

func TestBuildBatchPrompt_AutoDirectsPerArticleLanguage(t *testing.T) {
	got := buildBatchPrompt("[]", "auto")
	if !strings.Contains(got, "SAME language as the original article") {
		t.Errorf("auto prompt should direct per-article language, got %q", got)
	}
}

func TestBuildBatchPrompt_ZhMatchesHistoricalTemplate(t *testing.T) {
	// The zh rendering must equal the pre-parameterisation prompt so the
	// default behaviour is byte-for-byte unchanged.
	input := "[]"
	want := strings.Replace(batchPromptTemplate, "{{SUMMARY_RULE}}", summaryLangRule("zh"), 1)
	want = strings.Replace(want, "{{INPUT}}", input, 1)
	if got := buildBatchPrompt(input, "zh"); got != want {
		t.Errorf("zh prompt changed:\n got: %q\nwant: %q", got, want)
	}
}
