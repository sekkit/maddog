package skill

import (
	"strings"
	"unicode"
)

type MatchResult struct {
	Matched bool
	Skill   Skill
}

type Matcher struct {
	store *Store
}

func NewMatcher(store *Store) Matcher {
	return Matcher{store: store}
}

func (m Matcher) Match(task string) MatchResult {
	if m.store == nil {
		return MatchResult{}
	}
	taskTerms := termSet(task)
	if len(taskTerms) == 0 {
		return MatchResult{}
	}
	var best Skill
	bestScore := 0
	for _, sk := range m.store.List() {
		score := skillMatchScore(sk, taskTerms)
		if score > bestScore {
			bestScore = score
			best = sk
		}
	}
	if bestScore < 1 {
		return MatchResult{}
	}
	return MatchResult{Matched: true, Skill: best}
}

func skillMatchScore(sk Skill, taskTerms map[string]bool) int {
	score := 0
	for term := range termSet(sk.Name + " " + sk.Description) {
		if taskTerms[term] {
			score++
		}
	}
	return score
}

func termSet(text string) map[string]bool {
	out := map[string]bool{}
	for _, term := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	}) {
		if len(term) >= 3 {
			out[term] = true
		}
	}
	return out
}
