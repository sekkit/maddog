package skill

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type MatchResult struct {
	Matched bool
	Skill   Skill
	Score   int
	Reason  string
}

type Matcher struct {
	store     *Store
	MinScore  int
	MaxTokens int
}

func NewMatcher(store *Store) Matcher {
	return Matcher{store: store, MinScore: 4, MaxTokens: 24}
}

func (m Matcher) Match(task string) MatchResult {
	if m.store == nil {
		return MatchResult{}
	}
	taskTokens := tokenSet(task, m.MaxTokens)
	if len(taskTokens) == 0 {
		return MatchResult{}
	}
	minScore := m.MinScore
	if minScore <= 0 {
		minScore = 4
	}
	var best MatchResult
	for _, sk := range m.store.List() {
		if sk.Scope == ScopeBuiltin {
			continue
		}
		score := scoreSkill(taskTokens, sk)
		if score < minScore {
			continue
		}
		if !best.Matched || score > best.Score || (score == best.Score && sk.Name < best.Skill.Name) {
			best = MatchResult{
				Matched: true,
				Skill:   sk,
				Score:   score,
				Reason:  "matched existing skill " + sk.Name,
			}
		}
	}
	return best
}

func scoreSkill(taskTokens map[string]struct{}, sk Skill) int {
	skillTokens := tokenSet(sk.Name+" "+sk.Description, 64)
	canonicalSkillTokens := map[string]struct{}{}
	for tok := range skillTokens {
		canonicalSkillTokens[canonicalMatchToken(tok)] = struct{}{}
	}
	score := 0
	for tok := range taskTokens {
		if strings.EqualFold(tok, sk.Name) {
			score += 5
			continue
		}
		if _, ok := skillTokens[tok]; ok {
			score += 2
			continue
		}
		if _, ok := canonicalSkillTokens[canonicalMatchToken(tok)]; ok {
			score += 2
		}
	}
	return score
}

func canonicalMatchToken(tok string) string {
	switch tok {
	case "writing", "written", "writes":
		return "write"
	default:
		return tok
	}
}

func tokenSet(s string, max int) map[string]struct{} {
	out := map[string]struct{}{}
	for _, tok := range selectMatchTokens(tokenize(s), max) {
		if isStopToken(tok) {
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}

func selectMatchTokens(tokens []string, max int) []string {
	var filtered []string
	for _, tok := range tokens {
		if isStopToken(tok) {
			continue
		}
		filtered = append(filtered, tok)
	}
	if max <= 0 || len(filtered) <= max {
		return filtered
	}
	head := max / 2
	tail := max - head
	selected := make([]string, 0, max)
	selected = append(selected, filtered[:head]...)
	selected = append(selected, filtered[len(filtered)-tail:]...)
	return selected
}

func tokenize(s string) []string {
	s = strings.ToLower(norm.NFKC.String(s))
	var tokens []string
	var b strings.Builder
	var cjk []rune
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tokens = append(tokens, b.String())
		b.Reset()
	}
	flushCJK := func() {
		if len(cjk) == 0 {
			return
		}
		for _, r := range cjk {
			tokens = append(tokens, string(r))
		}
		for i := 0; i+1 < len(cjk); i++ {
			tokens = append(tokens, string(cjk[i:i+2]))
		}
		cjk = cjk[:0]
	}
	for _, r := range s {
		if isCJK(r) {
			flush()
			cjk = append(cjk, r)
			continue
		}
		flushCJK()
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flushCJK()
	flush()
	return tokens
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

func isStopToken(tok string) bool {
	if len([]rune(tok)) < 2 {
		return true
	}
	switch tok {
	case "the", "and", "for", "with", "this", "that", "from", "into", "task", "please", "implement", "update", "fix", "add", "make", "using":
		return true
	default:
		return false
	}
}
