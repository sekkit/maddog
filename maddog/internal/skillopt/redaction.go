package skillopt

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	optimizationSecretAssignment = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|auth[_-]?token|token|secret|password)\b\s*[:=]\s*[^\s,;]+`)
	optimizationBearer           = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	optimizationImageData        = regexp.MustCompile(`(?i)data:image/[A-Za-z0-9.+-]+;base64,[A-Za-z0-9+/=\r\n]+`)
)

func redactResult(value Result) Result {
	value.Output = redactOptimizationText(value.Output)
	if len(value.Evidence) > 0 {
		var decoded any
		if json.Unmarshal(value.Evidence, &decoded) == nil {
			if encoded, err := json.Marshal(redactJSONValue(decoded)); err == nil {
				value.Evidence = encoded
			}
		} else {
			value.Evidence = json.RawMessage(nil)
		}
	}
	return value
}

func redactOptimizationText(value string) string {
	value = optimizationImageData.ReplaceAllString(value, "[IMAGE_DATA_REDACTED]")
	value = optimizationBearer.ReplaceAllString(value, "Bearer [REDACTED]")
	return optimizationSecretAssignment.ReplaceAllStringFunc(value, func(match string) string {
		if key, _, ok := strings.Cut(match, ":"); ok {
			return strings.TrimSpace(key) + ": [REDACTED]"
		}
		if key, _, ok := strings.Cut(match, "="); ok {
			return strings.TrimSpace(key) + "=[REDACTED]"
		}
		return "[REDACTED]"
	})
}

func redactJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = redactJSONValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactJSONValue(item)
		}
		return out
	case string:
		return redactOptimizationText(typed)
	default:
		return value
	}
}
