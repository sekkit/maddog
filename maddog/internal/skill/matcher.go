package skill

import (
	"sort"
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
	return Matcher{store: store, MinScore: 2, MaxTokens: 24}
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
		minScore = 2
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
	score := 0
	for tok := range taskTokens {
		if _, ok := skillTokens[tok]; ok {
			score += 2
			continue
		}
		if strings.EqualFold(tok, sk.Name) {
			score += 3
		}
	}
	return score
}

func tokenSet(s string, max int) map[string]struct{} {
	tokens := tokenize(s)
	if max > 0 && len(tokens) > max {
		tokens = tokens[:max]
	}
	out := map[string]struct{}{}
	for _, tok := range tokens {
		if isStopToken(tok) {
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}

func tokenize(s string) []string {
	s = strings.ToLower(norm.NFKC.String(s))
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tokens = append(tokens, b.String())
		b.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	sort.Strings(tokens)
	return tokens
}

func isStopToken(tok string) bool {
	if len(tok) < 3 {
		return true
	}
	switch tok {
	case "the", "and", "for", "with", "this", "that", "from", "into", "task", "please", "implement", "update", "fix", "add", "make", "using":
		return true
	default:
		return false
	}
}
