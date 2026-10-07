//go:build windows

package settings

// GeminiModels lists free-tier text models compatible with dictionary JSON output.
// Verified against Google's Standard pricing on 2026-10-06. Project quotas vary.
var GeminiModels = []string{
	"gemini-3.1-flash-lite",
	"gemini-3.5-flash-lite",
	"gemini-3.8-flash",
	"gemini-3.7-flash",
	"gemini-3.6-flash",
	"gemini-3.5-flash",
	"gemini-3-flash-preview",
	"gemini-2.5-flash-lite",
	"gemini-2.5-flash",
	"gemini-2.5-pro",
}
