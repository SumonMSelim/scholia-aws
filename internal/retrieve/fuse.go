package retrieve

import "sort"

// fuse merges ranked id lists with reciprocal rank fusion.
// Each list is best-first and 1-based. A hit found by two legs outranks
// a hit found by one. Equal scores break toward the smaller id.
func fuse(lists [][]string, limit int) []string {
	scores := map[string]float64{}
	for _, list := range lists {
		for i, id := range list {
			if id == "" {
				continue
			}
			scores[id] += 1.0 / float64(rrfK+i+1)
		}
	}
	type row struct {
		id    string
		score float64
	}
	ranked := make([]row, 0, len(scores))
	for id, score := range scores {
		ranked = append(ranked, row{id: id, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].id < ranked[j].id
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]string, len(ranked))
	for i, row := range ranked {
		out[i] = row.id
	}
	return out
}
