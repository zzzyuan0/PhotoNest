package redaction

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var gpsPattern = regexp.MustCompile(`^-?\d{1,3}\.\d+\s*,\s*-?\d{1,3}\.\d+$`)

var sensitiveFragments = []string{
	"secret",
	"token",
	"password",
	"api_key",
	"apikey",
	"accesskey",
	"secretkey",
	"authorization",
	"presign",
	"signature",
	"gps",
	"coordinate",
	"ocr",
	"caption",
}

var signedURLQueryKeys = []string{
	"X-Amz-Signature",
	"X-Amz-Credential",
	"X-Amz-Security-Token",
	"signature",
	"token",
	"sign",
}

func RedactMap(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		if isSensitiveKey(key) {
			out[key] = maskedValue(key, value)
			continue
		}
		out[key] = RedactAny(value)
	}
	return out
}

func RedactAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return RedactMap(typed)
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, RedactAny(item))
		}
		return items
	case string:
		return RedactString(typed)
	default:
		return value
	}
}

func RedactString(value string) string {
	if gpsPattern.MatchString(strings.TrimSpace(value)) {
		return "[REDACTED:gps]"
	}

	if redacted, changed := redactSignedURL(value); changed {
		return redacted
	}

	return value
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	for _, fragment := range sensitiveFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func maskedValue(key string, value any) string {
	lowerKey := strings.ToLower(key)
	switch {
	case strings.Contains(lowerKey, "gps"), strings.Contains(lowerKey, "coordinate"):
		return "[REDACTED:gps]"
	case strings.Contains(lowerKey, "ocr"):
		return "[REDACTED:ocr]"
	case strings.Contains(lowerKey, "caption"):
		return "[REDACTED:caption]"
	default:
		return fmt.Sprintf("[REDACTED:%s]", key)
	}
}

func redactSignedURL(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value, false
	}

	query := parsed.Query()
	changed := false
	for _, key := range signedURLQueryKeys {
		if query.Has(key) {
			query.Set(key, "REDACTED")
			changed = true
		}
	}
	if !changed {
		return value, false
	}

	parsed.RawQuery = query.Encode()
	return parsed.String(), true
}
