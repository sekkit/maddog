package skillopt

import "strings"

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func terminalStatus(status RunStatus) bool {
	switch status {
	case StatusCompleted, StatusCanceled, StatusBudgetExhausted:
		return true
	default:
		return false
	}
}
