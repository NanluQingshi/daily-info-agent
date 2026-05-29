package processor

// systemPrompt is the shared system message sent in every batch processing call.
const systemPrompt = `You are a professional news analyst. You will receive a JSON array of news items.
For each item, return a JSON array with the same length, in the same order.
Output ONLY valid JSON — no markdown, no explanation, no code fences.`

// batchPromptTemplate is the user message template for batch processing.
// {{INPUT}} is replaced with the JSON array of simplified news items.
const batchPromptTemplate = `Analyse the following news items and return a JSON array of objects.
Each object must have exactly these fields:
  "url":               string  — copy from input, used for correlation
  "category":          string  — exactly one of: 金融, 政治, 经济, 科技/AI, 国际
  "summary":           string  — concise Chinese summary, 100-200 Chinese characters
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

// topicExtractionPromptTemplate is the user message for conversational topic extraction.
// {{MESSAGE}} is replaced with the user's raw message.
const topicExtractionPromptTemplate = `The user sent this message: "{{MESSAGE}}"

Return a JSON object with:
  "category": one of 金融, 政治, 经济, 科技/AI, 国际
  "keywords": array of 3-5 English search keywords suitable for a news query
  "summary":  one sentence describing what the user wants to know

Output ONLY valid JSON.`
