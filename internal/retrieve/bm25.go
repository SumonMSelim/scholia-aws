package retrieve

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/sumonmselim/scholia-aws/internal/domain"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
	// minMatched is the lexical floor: distinct query terms a child must share.
	minMatched = 2
)

// stopwords are dropped from queries. A match on them alone says nothing about the topic.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the and or but if of to in on at by for with from as is are was were be been being
		do does did doing have has had it its this that these those i me my we our you your he she they them their what which who whom
		whose when where why how can could should would will shall may might must not no yes so than then there here about into over
		under up down out just also very please tell explain give show help mean means`) {
		stopwords[w] = true
	}
}

// contentTerms is the query's distinct terms without stopwords. A query of
// only stopwords keeps them, so it can still match something.
func contentTerms(query string) []string {
	all := unique(tokens(query))
	var out []string
	for _, term := range all {
		if !stopwords[term] {
			out = append(out, term)
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

// rankBM25 orders children by Okapi BM25, best first. Terms are not stemmed.
// Common words are left out of the query, and a child must share at least two
// of the remaining terms (or all of them, when the query has fewer), so a
// question that is not about the course matches nothing.
func rankBM25(docs []domain.Chunk, query string, limit int) []domain.Chunk {
	terms := contentTerms(query)
	if len(terms) == 0 || len(docs) == 0 || limit < 1 {
		return nil
	}
	docTokens := make([][]string, len(docs))
	df := map[string]int{}
	var total int
	for i, doc := range docs {
		tok := tokens(doc.Text)
		docTokens[i] = tok
		total += len(tok)
		seen := map[string]struct{}{}
		for _, term := range tok {
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			df[term]++
		}
	}
	avg := float64(total) / float64(len(docs))
	if avg == 0 {
		return nil
	}
	n := float64(len(docs))
	need := minMatched
	if len(terms) < need {
		need = len(terms)
	}
	type scored struct {
		i     int
		score float64
	}
	var ranked []scored
	for i, tok := range docTokens {
		tf := map[string]int{}
		for _, term := range tok {
			tf[term]++
		}
		dl := float64(len(tok))
		var score float64
		matched := 0
		for _, term := range terms {
			freq := float64(tf[term])
			if freq == 0 {
				continue
			}
			matched++
			idf := math.Log(1 + (n-float64(df[term])+0.5)/(float64(df[term])+0.5))
			denom := freq + bm25K1*(1-bm25B+bm25B*dl/avg)
			score += idf * (freq * (bm25K1 + 1) / denom)
		}
		if score > 0 && matched >= need {
			ranked = append(ranked, scored{i: i, score: score})
		}
	}
	sort.Slice(ranked, func(a, b int) bool {
		if ranked[a].score != ranked[b].score {
			return ranked[a].score > ranked[b].score
		}
		return docs[ranked[a].i].ID < docs[ranked[b].i].ID
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]domain.Chunk, len(ranked))
	for i, row := range ranked {
		out[i] = docs[row.i]
	}
	return out
}

func tokens(text string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func unique(terms []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, term := range terms {
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}
