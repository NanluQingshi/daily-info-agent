package processor

import "strings"

// systemPrompt is the shared system message sent in every batch processing call.
const systemPrompt = `You are a professional news analyst. You will receive a JSON array of news items.
For each item, return a JSON array with the same length, in the same order.
Output ONLY valid JSON — no markdown, no explanation, no code fences.`

// batchPromptTemplate is the user message template for batch processing.
// {{INPUT}} is replaced with the JSON array of simplified news items;
// {{SUMMARY_RULE}} is replaced with the summary-language directive chosen
// by the SUMMARY_LANG configuration (zh / en / auto).
const batchPromptTemplate = `Analyse the following news items and return a JSON array of objects.
Each object must have exactly these fields:
  "url":               string  — copy from input, used for correlation
  "category":          string  — exactly one of: 金融, 政治, 经济, 科技/AI, 国际
  "summary":           string  — {{SUMMARY_RULE}}
  "credibility_score": number  — float 0.0-1.0 rating the source reliability of the domain
  "tags":              array   — up to 10 keyword strings (English or Chinese)
  "language":          string  — BCP-47 language code of the original article (e.g. "en", "zh")

Credibility scoring guidance:
  1.0 = authoritative government or major wire service (xinhua.net, reuters.com, bbc.com)
  0.8 = established mainstream media (theverge.com, people.com.cn)
  0.5 = mid-tier or regional outlet, content farm, or unverifiable source
  0.0 = known misinformation source or spam

Input items:
{{INPUT}}`

// summaryLangRule returns the directive text injected into the "summary"
// field description of batchPromptTemplate for the given language mode.
// Unknown modes fall back to Chinese (the historical behaviour).
func summaryLangRule(lang string) string {
	switch lang {
	case "en":
		return `concise English summary, 60-120 English words`
	case "auto":
		return `concise summary written in the SAME language as the original article (report the detected language in the "language" field; e.g. an English article gets an English summary, a Chinese article gets a Chinese summary of 100-200 characters)`
	default: // "zh" and anything unrecognised
		return `concise Chinese summary, 100-200 Chinese characters`
	}
}

// buildBatchPrompt renders batchPromptTemplate with the input JSON and the
// summary-language directive for lang.
func buildBatchPrompt(inputJSON, lang string) string {
	p := strings.Replace(batchPromptTemplate, "{{SUMMARY_RULE}}", summaryLangRule(lang), 1)
	return strings.Replace(p, "{{INPUT}}", inputJSON, 1)
}
