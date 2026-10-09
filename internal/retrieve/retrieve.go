// Package retrieve answers a question from one course.
//
// Two legs rank child chunks: BM25 over the chunk text stored for the
// course, and nearest neighbours from the vector index of one embedding
// model. Reciprocal rank fusion merges those ranks. The result is the
// parent chunk each child belongs to, because answers quote the parent.
package retrieve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/embed"
	"github.com/sumonmselim/scholia-aws/internal/vector"
)

const (
	// rrfK damps reciprocal rank fusion. Rank 1 scores 1/61.
	rrfK = 60
	// legLimit is how many children each leg may contribute before fusion.
	legLimit = 30
	// maxVectorDistance drops neighbours whose cosine similarity is under
	// 0.25. S3 Vectors cosine distance is 1 minus that similarity, and a
	// nearest-neighbour query always returns something.
	maxVectorDistance = 0.75
	maxLimit          = 20
	// perSource caps how many parents one source may give, so one long
	// document cannot crowd out the rest. Fusion over-fetches overFetch times
	// the limit so the cap still leaves enough; if not, the rest backfills.
	perSource = 2
	overFetch = 3
)

// Library lists the sources and chunks of one course.
// The table has no course-wide chunk index, so a search lists sources
// and then each source's chunks.
type Library interface {
	ListSources(ctx context.Context, courseID string) ([]domain.Source, error)
	ListChunks(ctx context.Context, sourceID string) ([]domain.Chunk, error)
}

// Searcher runs hybrid retrieval for one course.
type Searcher struct {
	Chunks  Library
	Vectors vector.Store
	Embed   embed.Embedder
}

// Search returns parent chunks for text, best first.
// modelID selects the vector index. limit is the maximum number of parents.
func (s *Searcher) Search(ctx context.Context, courseID, modelID, text string, limit int) ([]domain.Chunk, error) {
	if s.Chunks == nil || s.Vectors == nil || s.Embed == nil {
		return nil, errors.New("retrieve: library, vectors, and embedder are required")
	}
	courseID = strings.TrimSpace(courseID)
	modelID = strings.TrimSpace(modelID)
	text = strings.TrimSpace(text)
	if courseID == "" || modelID == "" || text == "" {
		return nil, errors.New("retrieve: course id, model id, and text are required")
	}
	if limit < 1 || limit > maxLimit {
		return nil, fmt.Errorf("retrieve: limit must be from 1 to %d", maxLimit)
	}

	chunks, err := s.courseChunks(ctx, courseID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Chunk, len(chunks))
	var children []domain.Chunk
	for _, chunk := range chunks {
		if chunk.CourseID != courseID {
			continue
		}
		byID[chunk.ID] = chunk
		if chunk.ParentID != "" {
			children = append(children, chunk)
		}
	}

	lexical := parentOrder(rankBM25(children, text, legLimit), byID)
	query, err := s.Embed.Embed(ctx, modelID, text)
	if err != nil {
		return nil, err
	}
	hits, err := s.Vectors.Query(ctx, modelID, query, courseID, legLimit)
	if err != nil {
		return nil, err
	}
	semantic := parentOrder(chunksFromHits(hits, courseID, byID), byID)
	ids := fuse([][]string{lexical, semantic}, limit*overFetch)

	ranked := make([]domain.Chunk, 0, len(ids))
	for _, id := range ids {
		parent, ok := byID[id]
		if !ok || parent.CourseID != courseID {
			continue
		}
		ranked = append(ranked, parent)
	}
	return capPerSource(ranked, limit), nil
}

// capPerSource keeps rank order, takes at most perSource parents from each
// source, then backfills from what was skipped when fewer than limit remain.
func capPerSource(ranked []domain.Chunk, limit int) []domain.Chunk {
	out := make([]domain.Chunk, 0, limit)
	var skipped []domain.Chunk
	count := map[string]int{}
	for _, chunk := range ranked {
		if len(out) == limit {
			break
		}
		if count[chunk.SourceID] >= perSource {
			skipped = append(skipped, chunk)
			continue
		}
		count[chunk.SourceID]++
		out = append(out, chunk)
	}
	for _, chunk := range skipped {
		if len(out) == limit {
			break
		}
		out = append(out, chunk)
	}
	return out
}

func (s *Searcher) courseChunks(ctx context.Context, courseID string) ([]domain.Chunk, error) {
	sources, err := s.Chunks.ListSources(ctx, courseID)
	if err != nil {
		return nil, err
	}
	var all []domain.Chunk
	for _, src := range sources {
		chunks, err := s.Chunks.ListChunks(ctx, src.ID)
		if err != nil {
			return nil, err
		}
		all = append(all, chunks...)
	}
	return all, nil
}

// chunksFromHits keeps neighbours inside the course that are close
// enough to cite, nearest first.
func chunksFromHits(hits []vector.Hit, courseID string, byID map[string]domain.Chunk) []domain.Chunk {
	ordered := append([]vector.Hit(nil), hits...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Distance != ordered[j].Distance {
			return ordered[i].Distance < ordered[j].Distance
		}
		return ordered[i].ChunkID < ordered[j].ChunkID
	})
	var out []domain.Chunk
	for _, hit := range ordered {
		if hit.CourseID != courseID || float64(hit.Distance) > maxVectorDistance {
			continue
		}
		chunk, ok := byID[hit.ChunkID]
		if !ok || chunk.CourseID != courseID {
			continue
		}
		out = append(out, chunk)
	}
	return out
}

// parentOrder maps a child ranking onto parent ids, keeping the best
// rank when several children share a parent.
func parentOrder(ranked []domain.Chunk, byID map[string]domain.Chunk) []string {
	var ids []string
	seen := map[string]bool{}
	for _, chunk := range ranked {
		id := chunk.ParentID
		if id == "" {
			id = chunk.ID
		}
		if seen[id] {
			continue
		}
		parent, ok := byID[id]
		if !ok || parent.ParentID != "" {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}
