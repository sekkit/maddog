package review

import "fmt"

func summarizeReport(report Report) string {
	if len(report.Findings) == 0 {
		if report.LargeDiff {
			return fmt.Sprintf("No deterministic review findings. large diff: %d added lines reviewed.", report.DiffLines)
		}
		return "No deterministic review findings."
	}
	summary := fmt.Sprintf("%d deterministic review finding(s).", len(report.Findings))
	if report.LargeDiff {
		summary += fmt.Sprintf(" large diff: %d added lines reviewed.", report.DiffLines)
	}
	if report.Fallback == "diff_only" {
		summary += " Code backend unavailable; used diff-only fallback."
	}
	return summary
}
