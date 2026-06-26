package strmatch

import (
	"strings"
	"unicode"
)

// Tunable weights for score
const (
	containmentBase = 0.50 // floor for any substring containment
	containmentSpan = 0.40 // added across coverage in (0,1]
	wholeTokenBonus = 0.07 // containment whose match lands on token boundaries both sides
	midWordPenalty  = 0.15 // containment buried inside a larger word on both sides
	containmentCap  = 0.99 // keep containment strictly below an exact match

	tokenBase = 0.50 // floor when at least one whole candidate token is found
	tokenSpan = 0.45 // added across the fraction of candidate tokens found

	editWeight = 0.60 // edit-distance similarity is scaled below the other bands
)

// Scored candidate returned by Best / BestAbove
type Match struct {
	Value string  // the candidate string
	Index int     // its position in the input slice (-1 when no match)
	Score float64 // similarity to the query, in [0,1]
}

// Determines how closely query matches candidate, in [0,1], where 1 means an exact (case-insensitive) match and 0 means no (meaningful) similarity
func Score(query, candidate string) float64 {
	q := normalize(query)
	c := normalize(candidate)
	if q == "" || c == "" {
		return 0
	}
	if q == c {
		return 1.0
	}

	best := containmentScore(q, c)
	if s := tokenScore(q, c); s > best {
		best = s
	}
	if s := editScore(q, c); s > best {
		best = s
	}
	return best
}

// Gets highest scoring candidate for query
func Best(query string, candidates []string) (Match, bool) {
	best := Match{Index: -1}
	found := false
	for i, c := range candidates {
		s := Score(query, c)
		if !found || s > best.Score {
			best = Match{Value: c, Index: i, Score: s}
			found = true
		}
	}
	return best, found
}

// Best filtered by a minimum score
func BestAbove(query string, candidates []string, min float64) (Match, bool) {
	m, ok := Best(query, candidates)
	if !ok || m.Score < min {
		return Match{Index: -1}, false
	}
	return m, true
}

// Best for an arbitrary item type
func BestFunc[T any](query string, items []T, key func(T) string) (best T, score float64, ok bool) {
	for i, it := range items {
		s := Score(query, key(it))
		if i == 0 || s > score {
			best, score, ok = it, s, true
		}
	}
	return best, score, ok
}

// Scores the case where one string is a substring of the other, weighted by coverage of the longer string and how cleanly it matches on token bounds
func containmentScore(q, c string) float64 {
	short, long := q, c
	if len(long) < len(short) {
		short, long = long, short
	}
	idx := bestContainmentIndex(long, short)
	if idx < 0 {
		return 0
	}

	coverage := float64(len(short)) / float64(len(long))
	score := containmentBase + containmentSpan*coverage

	leftBoundary := idx == 0 || !isWordByte(long[idx-1])
	rightBoundary := idx+len(short) == len(long) || !isWordByte(long[idx+len(short)])
	switch {
	case leftBoundary && rightBoundary:
		// Matched a complete token (e.g. "neoforge" within "neoforge-21.1.228").
		score += wholeTokenBonus
	case !leftBoundary && !rightBoundary:
		// Buried inside a larger word; very likely a spurious fragment.
		score -= midWordPenalty
	}
	return clamp(score, 0, containmentCap)
}

// Ger offset of the occurrence of short within long that sits in most boundaries
func bestContainmentIndex(long, short string) int {
	best, bestRank := -1, -1
	for start := 0; ; {
		i := strings.Index(long[start:], short)
		if i < 0 {
			break
		}
		idx := start + i
		rank := 0
		if idx == 0 || !isWordByte(long[idx-1]) {
			rank++
		}
		if idx+len(short) == len(long) || !isWordByte(long[idx+len(short)]) {
			rank++
		}
		if rank > bestRank {
			best, bestRank = idx, rank
			if rank == 2 {
				break
			}
		}
		start = idx + 1
	}
	return best
}

// Rewards candidates whose whole words all appear as whole words in query
func tokenScore(q, c string) float64 {
	candidateTokens := tokens(c)
	if len(candidateTokens) == 0 {
		return 0
	}
	queryTokens := tokenSet(q)
	matched := 0
	for _, t := range candidateTokens {
		if queryTokens[t] {
			matched++
		}
	}
	if matched == 0 {
		return 0
	}
	frac := float64(matched) / float64(len(candidateTokens))
	return tokenBase + tokenSpan*frac
}

// Turns Levenshtein distance into a similarity in [0, editWeight] - typo tolerance
func editScore(q, c string) float64 {
	maxLen := len(q)
	if len(c) > maxLen {
		maxLen = len(c)
	}
	if maxLen == 0 {
		return 0
	}
	sim := 1.0 - float64(levenshtein(q, c))/float64(maxLen)
	if sim <= 0 {
		return 0
	}
	return editWeight * sim
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Splits s into runs of letters and digits, discarding separators
func tokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func tokenSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, t := range tokens(s) {
		set[t] = true
	}
	return set
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// Computes the edit distance between a and b - rune aware
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
