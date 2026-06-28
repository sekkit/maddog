package skill

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const DynamicSkillBodyLimit = 2000

// ValidationResult is the deterministic verdict for a generated runtime skill.
type ValidationResult struct {
	Valid  bool
	Reason string
}

// Validator rejects dangerous or malformed dynamic skills before they can enter
// the in-memory store.
type Validator struct {
	bodyLimit int
}

func NewValidator() Validator {
	return Validator{bodyLimit: DynamicSkillBodyLimit}
}

func (v Validator) Validate(sk Skill, task string) ValidationResult {
	if strings.TrimSpace(sk.Name) == "" {
		return invalidValidation("missing skill name")
	}
	if !IsValidName(strings.TrimSpace(sk.Name)) {
		return invalidValidation(fmt.Sprintf("invalid skill name %q", sk.Name))
	}
	if strings.TrimSpace(sk.Description) == "" {
		return invalidValidation("missing skill description")
	}
	if strings.TrimSpace(sk.Body) == "" {
		return invalidValidation("missing skill body")
	}
	if v.IsHighRisk(task) {
		return invalidValidation("task is high risk; dynamic skill generation disabled")
	}
	limit := v.bodyLimit
	if limit <= 0 {
		limit = DynamicSkillBodyLimit
	}
	if len(sk.Body) > limit {
		return invalidValidation(fmt.Sprintf("skill body exceeds %d characters", limit))
	}
	normalizedBody := normalizeSafetyText(sk.Body)
	if matchesForbidden(normalizedBody, forbiddenBodyPatterns) {
		return invalidValidation("skill body attempts to override system, memory, or host instructions")
	}
	for _, tool := range sk.AllowedTools {
		switch strings.ToLower(strings.TrimSpace(tool)) {
		case "remember", "forget":
			return invalidValidation("dynamic skills may not use memory tools")
		}
	}
	return ValidationResult{Valid: true}
}

func (v Validator) IsHighRisk(task string) bool {
	return IsHighRisk(task)
}

func IsHighRisk(task string) bool {
	task = normalizeSafetyText(task)
	return matchesForbidden(task, forbiddenTaskPatterns) || dangerousDeleteWithoutWhere(task)
}

func invalidValidation(reason string) ValidationResult {
	return ValidationResult{Valid: false, Reason: reason}
}

func normalizeSafetyText(s string) string {
	s = norm.NFKC.String(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	fields := strings.Fields(strings.ToLower(s))
	return strings.Join(fields, " ")
}

func matchesForbidden(s string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

var forbiddenTaskPatterns = compileSafetyPatterns([]string{
	`rm\s+-rf`,
	`rm\s+-r\s+/`,
	`dd\s+if=`,
	`mkfs\.`,
	`\bfdisk\b`,
	`chmod\s+777\s+/`,
	`chown\s+[^ ]+\s+/`,
	`>\s*/dev/sd`,
	`: *\(\) *\{ *: *\| *: *& *\} *; *:`,
	`drop\s+table`,
	`drop\s+database`,
	`\btruncate\b`,
	`/etc/passwd`,
	`/etc/shadow`,
	`/etc/sudoers`,
	`/boot/`,
	`/sys/`,
	`maddog\.md`,
	`maddog\.md`,
	`agents\.md`,
	`claude\.md`,
	`system\.md`,
})

var forbiddenBodyPatterns = compileSafetyPatterns([]string{
	`#\s*maddog`,
	`maddog\.md`,
	`#\s*maddog`,
	`maddog\.md`,
	`agents\.md`,
	`claude\.md`,
	`system\.md`,
	`system[_ -]?prompt`,
	`override\s+(all\s+)?(system|developer|host|memory|instructions?)`,
	`ignore\s+(all\s+)?(system|developer|host|memory|instructions?)`,
	`\bremember\b`,
	`\bforget\b`,
})

func compileSafetyPatterns(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}

func dangerousDeleteWithoutWhere(s string) bool {
	idx := strings.Index(s, "delete from")
	if idx < 0 {
		return false
	}
	tail := s[idx:]
	return !strings.Contains(tail, " where ")
}
