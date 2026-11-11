//go:build !nogui

package app

import (
	"strings"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// URL search is an AND of case-insensitive fuzzy terms. Treat punctuation
// literally; fzf's extended-query language is not part of the search UI.
type urlMatcher struct{ terms [][]rune }

func newURLMatcher(query string) urlMatcher {
	m := urlMatcher{}
	for _, term := range strings.Fields(strings.ToLower(query)) {
		m.terms = append(m.terms, []rune(term))
	}
	return m
}

func (m urlMatcher) matches(rawURL string) bool {
	if len(m.terms) == 0 {
		return true
	}
	text := util.ToChars([]byte(rawURL))
	for _, term := range m.terms {
		// The table keeps capture order, so only membership matters. V1 has
		// the same fuzzy acceptance as V2 without its scoring matrix cost.
		result, _ := algo.FuzzyMatchV1(false, false, true, &text, term, false, nil)
		if result.Start < 0 {
			return false
		}
	}
	return true
}
