//go:build windows

package settings

import (
	"touchdict/internal/chatgpt"
	"touchdict/internal/online"
)

// GeminiModels lists free-tier text models compatible with dictionary JSON output.
// Verified against Google's Standard pricing on 2026-10-06. Project quotas vary.
var GeminiModels = []string{
	"gemini-3.1-flash-lite",
	"gemini-3.8-flash",
	"gemini-3.6-flash",
	"gemini-3-flash-preview",
}

func OnlineModels() []string {
	names := append([]string(nil), GeminiModels...)
	names = append(names, "groq:openai/gpt-oss-20b", "groq:openai/gpt-oss-120b", "groq:qwen/qwen3.8-27b", "openrouter:openrouter/free")
	for _, m := range chatgpt.Default.Models() {
		names = append(names, "chatgpt:"+m.Slug)
	}
	return names
}

func (c Config) KeyForModel(name string) string {
	switch online.Provider(name) {
	case "ChatGPT 订阅":
		return ""
	case "Groq":
		return c.GroqAPIKey
	case "OpenRouter":
		return c.OpenRouterAPIKey
	default:
		return c.APIKey
	}
}

func (c Config) ModelKeys(names []string) map[string]string {
	keys := make(map[string]string, len(names))
	for _, name := range names {
		keys[name] = c.KeyForModel(name)
	}
	return keys
}

func NormalizeGeminiModel(name string) string {
	if name == "gemini-3.7-flash" {
		return "gemini-3.8-flash"
	}
	if name == "gemini-2.5-flash-lite" || name == "gemini-2.5-flash" || name == "gemini-2.5-pro" {
		return "gemini-3.1-flash-lite"
	}
	if name == "gemini-3.5-flash" || name == "gemini-3.5-flash-lite" {
		return "gemini-3.6-flash"
	}
	return name
}
