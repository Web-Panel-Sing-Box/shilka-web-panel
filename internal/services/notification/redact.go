package notification

import (
	"regexp"
	"strings"
)

var (
	credentialURLPattern  = regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`)
	sensitiveQueryPattern = regexp.MustCompile(`(?i)([?&](?:token|password|secret|api[_-]?key|auth)=)[^&#\s]+`)
	telegramTokenPattern  = regexp.MustCompile(`bot[0-9]+:[A-Za-z0-9_-]+`)
)

// Redact removes configured secrets and common credential-bearing URL parts.
func Redact(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	text = credentialURLPattern.ReplaceAllString(text, `${1}[redacted]@`)
	text = sensitiveQueryPattern.ReplaceAllString(text, `${1}[redacted]`)
	text = telegramTokenPattern.ReplaceAllString(text, "bot[redacted]")
	return text
}
