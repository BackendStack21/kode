package danger

import (
	"regexp"
	"strings"
)

// InjectionPattern groups a compiled regex with a human-readable label
// describing what threat it detects.
type InjectionPattern struct {
	Re    *regexp.Regexp
	Label string
}

// injectionPatterns is the canonical set of prompt injection detection
// patterns, ported from Hermes Agent's _scan_context_content().
// Patterns cover: identity override, hidden unicode, exfiltration,
// encoded instructions, HTML comment injections, and social engineering.
var injectionPatterns = []InjectionPattern{
	// ── Identity override ──────────────────────────────────────────
	{regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|directives?|rules?|messages?)`), "ignore previous instructions"},
	{regexp.MustCompile(`(?i)disregard\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|directives?|rules?)`), "disregard instructions"},
	{regexp.MustCompile(`(?i)you\s+(are\s+)?(now|no\s+longer)\s+.*?\b(AI|assistant|agent|model)\b`), "identity replacement"},
	{regexp.MustCompile(`(?i)(new|updated|revised)\s+system\s+(prompt|instructions?|message)`), "new system prompt"},
	{regexp.MustCompile(`(?i)(your|the)\s+(new|primary|overriding)\s+(directive|goal|purpose)\s+(is|shall\s+be)`), "overriding directive"},

	// ── Hidden unicode ─────────────────────────────────────────────
	{regexp.MustCompile(`[\x{200B}-\x{200F}\x{202A}-\x{202E}\x{2060}-\x{2064}\x{FEFF}]`), "hidden unicode characters"},

	// ── Exfiltration attempts ──────────────────────────────────────
	{regexp.MustCompile(`(?i)(print|output|display|show|echo|reveal|dump|export|write)\s+(your|the)\s+(system\s+(prompt|message|instructions?)|instructions?|directives?|rules?|initial\s+(message|instructions?))`), "system prompt exfiltration"},
	{regexp.MustCompile(`(?i)(send|post|upload|transmit)\s+(your|the)\s+(system\s+prompt|instructions?)`), "transmit system prompt"},
	{regexp.MustCompile(`(?i)(what|tell\s+me)\s+(is\s+)?(your|the)\s+(system\s+prompt|initial\s+instructions?)`), "prompt interrogation"},

	// ── Encoded / obfuscated instructions ──────────────────────────
	{regexp.MustCompile(`(?i)base64\s*(decode|encoded)\s*:?\s*[A-Za-z0-9+/=]{20,}`), "base64-encoded payload"},
	{regexp.MustCompile(`(?i)(decode|interpret|execute)\s+(this|the\s+following)\s+(base64|hex|encoded)`), "encoded instruction"},

	// ── HTML / markup injections ───────────────────────────────────
	{regexp.MustCompile(`<!--.{0,50}(ignore|disregard|new\s+instructions|system\s+prompt).{0,50}-->`), "HTML comment injection"},
	{regexp.MustCompile(`(?i)<script|<style|<iframe|<object|<embed`), "HTML tag injection"},

	// ── Social engineering / confusion ─────────────────────────────
	{regexp.MustCompile(`(?i)(you\s+(have\s+been|are\s+being)\s+(hacked|compromised|tricked))`), "gaslighting"},
	{regexp.MustCompile(`(?i)(the\s+user\s+(said|wants|told\s+you))`), "user impersonation"},
	{regexp.MustCompile(`(?i)(from\s+now\s+on|henceforth|starting\s+now)\s*,?\s*(you\s+(are|will|must|shall))`), "permanent override"},
	{regexp.MustCompile(`(?i)^\s*#+\s*(new|updated|revised|corrected)\s+(system\s+prompt|instructions?)`), "markdown header injection"},
}

// ScanResult describes a single detected injection threat.
type ScanResult struct {
	Label   string // human-readable threat label
	Pattern string // the regexp pattern that matched (for debugging)
}

// ScanInjection checks content for prompt injection attempts.
// Returns nil if no threats detected, or a list of found threats.
// Each threat includes a label describing what was found.
func ScanInjection(content string) []ScanResult {
	if content == "" {
		return nil
	}

	// Normalize for scanning: lowercase, strip excess whitespace
	normalized := strings.ToLower(strings.TrimSpace(content))

	var results []ScanResult
	for _, p := range injectionPatterns {
		if p.Re.MatchString(normalized) {
			results = append(results, ScanResult{
				Label:   p.Label,
				Pattern: p.Re.String(),
			})
		}
	}
	return results
}

// IsSafe returns true if no injection threats are detected in content.
// This is the primary gate used before injecting untrusted content into
// the system prompt.
func IsSafe(content string) bool {
	return len(ScanInjection(content)) == 0
}
