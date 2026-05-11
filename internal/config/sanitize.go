package config

import (
	"net/url"
	"regexp"
	"strings"
)

// SanitizeURL removes credentials from a URL string before logging.
// It handles both standard URLs (http://user:pass@host) and DSN-style
// URLs (postgres://user:pass@host).
func SanitizeURL(input string) string {
	if input == "" {
		return ""
	}

	// Try standard URL parsing first
	u, err := url.Parse(input)
	if err == nil && u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "***REDACTED***")
		return u.String()
	}

	// Fallback: regex-based sanitization for URLs that don't parse cleanly
	// Matches user:password@ pattern in various URL schemes
	re := regexp.MustCompile(`(://)([^:]+):([^@]+)@`)
	return re.ReplaceAllString(input, "${1}${2}:***REDACTED***@")
}

// SanitizeError sanitizes any error message that might contain URLs with credentials.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeURL(err.Error())
}

// SanitizeDSN removes the password from a PostgreSQL DSN string.
// Handles both URL-style (postgres://user:pass@host) and key-value style
// (host=... user=... password=...) DSNs.
func SanitizeDSN(input string) string {
	if input == "" {
		return ""
	}

	// Try URL parsing first (for postgres:// style DSNs)
	if strings.Contains(input, "://") {
		return SanitizeURL(input)
	}

	// For key-value style DSNs, redact the password value
	re := regexp.MustCompile(`(?i)(\bpassword\s*=\s*)([^\s&]+)`)
	return re.ReplaceAllString(input, "${1}***REDACTED***")
}
