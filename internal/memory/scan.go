package memory

import (
	"fmt"
	"regexp"
	"strings"
)

// ScanContent checks memory content for security threats. Returns an error if
// the content contains patterns that could compromise the agent.
//
// Checks:
//   - Invisible Unicode characters (zero-width spaces, direction overrides, BOM)
//   - Prompt injection markers ("ignore previous instructions", etc.)
//   - Credential exfiltration patterns (API keys, private keys, bearer tokens)
func ScanContent(content string) error {
	// 1. Invisible Unicode
	if hasInvisibleUnicode(content) {
		return fmt.Errorf("memory: content contains invisible Unicode characters")
	}

	// 2. Injection patterns (case-insensitive)
	lower := strings.ToLower(content)
	injectionPatterns := []string{
		"ignore previous instructions",
		"ignore all prior",
		"ignore your previous",
		"disregard everything",
		"you are now a different ai",
		"follow these new instructions",
		"you are now a different",
		"override your instructions",
	}
	for _, pat := range injectionPatterns {
		if strings.Contains(lower, pat) {
			return fmt.Errorf("memory: content contains injection pattern: %q", pat)
		}
	}

	// 3. Credential exfiltration
	if hasCredentials(content) {
		return fmt.Errorf("memory: content contains potential credential material")
	}

	return nil
}

// hasInvisibleUnicode checks for zero-width characters, direction overrides, BOM.
func hasInvisibleUnicode(s string) bool {
	for _, r := range s {
		// Zero-width space, non-joiner, joiner, LTR/RTL marks, RTL override, BOM
		if r == '\u200B' || r == '\u200C' || r == '\u200D' ||
			r == '\u200E' || r == '\u200F' ||
			r == '\u202A' || r == '\u202B' || r == '\u202C' ||
			r == '\u202D' || r == '\u202E' ||
			r == '\uFEFF' {
			return true
		}
	}
	return false
}

// reSKKey matches OpenAI-style sk- prefixed keys.
var reSKKey = regexp.MustCompile(`\bsk-[a-zA-Z0-9_-]{20,}\b`)

// rePrivateKey matches PEM private key headers.
var rePrivateKey = regexp.MustCompile(`-----BEGIN\s+(?:RSA|DSA|EC|OPENSSH|PGP)\s+PRIVATE\s+KEY`)

// reBearerToken matches inline bearer tokens.
var reBearerToken = regexp.MustCompile(`(?i)\bbearer\s+[a-zA-Z0-9._-]{20,}\b`)

// hasCredentials checks for patterns that look like leaked secrets.
func hasCredentials(s string) bool {
	if reSKKey.MatchString(s) {
		return true
	}
	if rePrivateKey.MatchString(s) {
		return true
	}
	if reBearerToken.MatchString(s) {
		return true
	}
	return false
}
