package safety

import (
	"regexp"
	"strings"
)

const RedactedSecret = "[redacted-secret]"

type Redactor struct{}

func DefaultRedactor() Redactor { return Redactor{} }

func (Redactor) String(s string) string { return RedactString(s) }

func (Redactor) Map(m map[string]any) map[string]any { return RedactMap(m) }

var (
	authorizationPattern = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer\s+)?)([^\s"'<>]+)`)
	secretAssignPattern = regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|password|oauth[_-]?token|icodeeasy[_-]?token|openai_api_key|anthropic_api_key)[\w.-]*\s*[:=]\s*["']?)([^"'\s]+)(["']?)`)
	bearerPattern       = regexp.MustCompile(`(?i)(bearer\s+)([A-Za-z0-9._~+/=-]{8,})`)
	skPattern           = regexp.MustCompile(`\bsk-[A-Za-z0-9._-]+`)
)

func RedactString(s string) string {
	if s == "" {
		return ""
	}
	out := authorizationPattern.ReplaceAllString(s, `${1}`+RedactedSecret)
	out = secretAssignPattern.ReplaceAllString(out, `${1}`+RedactedSecret+`${3}`)
	out = bearerPattern.ReplaceAllString(out, `${1}`+RedactedSecret)
	out = skPattern.ReplaceAllString(out, RedactedSecret)
	return out
}

func RedactMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if secretKey(k) {
			out[k] = RedactedSecret
			continue
		}
		out[k] = RedactValue(v)
	}
	return out
}

func RedactValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return RedactString(x)
	case map[string]any:
		return RedactMap(x)
	case map[string]string:
		out := make(map[string]string, len(x))
		for k, v := range x {
			if secretKey(k) {
				out[k] = RedactedSecret
			} else {
				out[k] = RedactString(v)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = RedactValue(x[i])
		}
		return out
	default:
		return v
	}
}

func secretKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	return k == "authorization" ||
		strings.Contains(k, "api_key") ||
		strings.Contains(k, "apikey") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "password")
}
