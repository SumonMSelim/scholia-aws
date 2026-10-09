package retrieve

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/locator"
	"github.com/sumonmselim/scholia-aws/internal/vector"
)

const fixtureModel = "test-embed"

func TestBM25RanksSharedTermsFirst(t *testing.T) {
	docs := []domain.Chunk{
		{ID: "c-tcp", ParentID: "p-tcp", Text: "TCP retransmits lost segments."},
		{ID: "c-route", ParentID: "p-route", Text: "Tables pick the next hop for a packet."},
		{ID: "c-dns", ParentID: "p-dns", Text: "DNS resolves a name to an address."},
	}
	got := rankBM25(docs, "next hop", 5)
	if len(got) != 1 || got[0].ID != "c-route" {
		t.Fatalf("rank = %+v", idsOf(got))
	}
}

func TestFusePrefersTwoLegsAndBreaksTiesByID(t *testing.T) {
	// a is rank 1 on one leg only. b is rank 2 on both, which still
	// beats a single rank-1 hit: 1/62 + 1/62 > 1/61.
	got := fuse([][]string{{"a", "b"}, {"b", "c"}}, 3)
	if len(got) != 3 || got[0] != "b" || got[1] != "a" || got[2] != "c" {
		t.Fatalf("fuse = %v", got)
	}
	tied := fuse([][]string{{"b", "a"}, {"a", "b"}}, 2)
	if len(tied) != 2 || tied[0] != "a" || tied[1] != "b" {
		t.Fatalf("tie break = %v", tied)
	}
}

func TestSearchFixture(t *testing.T) {
	lib, index := loadFixture(t)

	lexical := queryVector{vec: []float32{0, 0, 0, 1}}
	got := mustSearch(t, &Searcher{Chunks: lib, Vectors: index, Embed: lexical}, "nets", "next hop")
	if got[0].ID != "p-route" || got[0].Text != "Routing" || got[0].Locators[0].Slide != 1 {
		t.Fatalf("lexical top = %+v", got[0])
	}
	assertCourse(t, got, "nets")

	semantic := queryVector{vec: []float32{1, 0, 0, 0}}
	got = mustSearch(t, &Searcher{Chunks: lib, Vectors: index, Embed: semantic}, "nets", "forwarding decision")
	if got[0].ID != "p-route" {
		t.Fatalf("semantic top = %+v", got[0])
	}
	for _, chunk := range got {
		if chunk.ID == "c-route" {
			t.Fatal("search returned a child chunk")
		}
	}
}

func TestSearchStaysInCourse(t *testing.T) {
	lib, index := loadFixture(t)
	embedder := queryVector{vec: []float32{1, 0, 0, 0}}
	nets := mustSearch(t, &Searcher{Chunks: lib, Vectors: index, Embed: embedder}, "nets", "next hop")
	assertCourse(t, nets, "nets")
	for _, chunk := range nets {
		if chunk.ID == "p-other" {
			t.Fatal("course nets returned the other course")
		}
	}
	other := mustSearch(t, &Searcher{Chunks: lib, Vectors: index, Embed: embedder}, "other", "next hop")
	if len(other) != 1 || other[0].ID != "p-other" {
		t.Fatalf("other = %+v", other)
	}
}

func TestSearchDropsDistantNeighbours(t *testing.T) {
	lib, index := loadFixture(t)
	embedder := queryVector{vec: []float32{0, 0, 0, 1}}
	got, err := (&Searcher{Chunks: lib, Vectors: index, Embed: embedder}).Search(context.Background(), "nets", fixtureModel, "zzzz", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("distant neighbours = %+v", got)
	}
}

func TestSearchRejects(t *testing.T) {
	lib, index := loadFixture(t)
	search := &Searcher{Chunks: lib, Vectors: index, Embed: queryVector{vec: []float32{1, 0, 0, 0}}}
	if _, err := search.Search(context.Background(), "", fixtureModel, "next hop", 5); err == nil {
		t.Fatal("empty course id was accepted")
	}
	if _, err := search.Search(context.Background(), "nets", fixtureModel, "next hop", 0); err == nil {
		t.Fatal("limit 0 was accepted")
	}
	if _, err := (&Searcher{}).Search(context.Background(), "nets", fixtureModel, "next hop", 5); err == nil {
		t.Fatal("nil dependencies were accepted")
	}
}

type queryVector struct {
	vec []float32
	err error
}

func (q queryVector) Embed(context.Context, string, string) ([]float32, error) {
	if q.err != nil {
		return nil, q.err
	}
	return q.vec, nil
}

type memLib struct {
	sources map[string][]domain.Source
	chunks  map[string][]domain.Chunk
}

func (m memLib) ListSources(_ context.Context, courseID string) ([]domain.Source, error) {
	return m.sources[courseID], nil
}

func (m memLib) ListChunks(_ context.Context, sourceID string) ([]domain.Chunk, error) {
	return m.chunks[sourceID], nil
}

type memVec struct {
	items []stored
}

type stored struct {
	model string
	v     vector.Vector
}

func (m *memVec) Put(_ context.Context, modelID string, vectors []vector.Vector) error {
	for _, v := range vectors {
		m.items = append(m.items, stored{model: modelID, v: v})
	}
	return nil
}

func (m *memVec) Query(_ context.Context, modelID string, query []float32, courseID string, limit int) ([]vector.Hit, error) {
	var hits []vector.Hit
	for _, item := range m.items {
		if item.model != modelID || item.v.CourseID != courseID {
			continue
		}
		hits = append(hits, vector.Hit{
			ChunkID:  item.v.ChunkID,
			CourseID: item.v.CourseID,
			ParentID: item.v.ParentID,
			Locators: item.v.Locators,
			Distance: cosineDistance(query, item.v.Values),
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Distance != hits[j].Distance {
			return hits[i].Distance < hits[j].Distance
		}
		return hits[i].ChunkID < hits[j].ChunkID
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func cosineDistance(a, b []float32) float32 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 1
	}
	return float32(1 - dot/(math.Sqrt(na)*math.Sqrt(nb)))
}

func loadFixture(t *testing.T) (memLib, *memVec) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "course.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Courses []struct {
			ID      string `json:"id"`
			Sources []struct {
				ID     string `json:"id"`
				Chunks []struct {
					ID       string            `json:"id"`
					ParentID string            `json:"parent_id"`
					Text     string            `json:"text"`
					Locators []locator.Locator `json:"locators"`
					Values   []float32         `json:"values"`
				} `json:"chunks"`
			} `json:"sources"`
		} `json:"courses"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	lib := memLib{sources: map[string][]domain.Source{}, chunks: map[string][]domain.Chunk{}}
	index := &memVec{}
	for _, course := range file.Courses {
		for _, src := range course.Sources {
			lib.sources[course.ID] = append(lib.sources[course.ID], domain.Source{ID: src.ID, CourseID: course.ID})
			for _, chunk := range src.Chunks {
				item := domain.Chunk{
					ID: chunk.ID, CourseID: course.ID, SourceID: src.ID,
					ParentID: chunk.ParentID, Text: chunk.Text, Locators: chunk.Locators,
				}
				lib.chunks[src.ID] = append(lib.chunks[src.ID], item)
				if len(chunk.Values) == 0 {
					continue
				}
				if err := index.Put(context.Background(), fixtureModel, []vector.Vector{{
					CourseID: course.ID, ChunkID: chunk.ID, ParentID: chunk.ParentID,
					Locators: chunk.Locators, Values: chunk.Values,
				}}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return lib, index
}

func mustSearch(t *testing.T, s *Searcher, courseID, text string) []domain.Chunk {
	t.Helper()
	got, err := s.Search(context.Background(), courseID, fixtureModel, text, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no results")
	}
	return got
}

func assertCourse(t *testing.T, chunks []domain.Chunk, courseID string) {
	t.Helper()
	for _, chunk := range chunks {
		if chunk.CourseID != courseID {
			t.Fatalf("chunk %s is in %s", chunk.ID, chunk.CourseID)
		}
	}
}

func idsOf(chunks []domain.Chunk) []string {
	out := make([]string, len(chunks))
	for i, chunk := range chunks {
		out[i] = chunk.ID
	}
	return out
}

func TestBM25Floor(t *testing.T) {
	docs := []domain.Chunk{
		{ID: "c-tcp", ParentID: "p-tcp", Text: "TCP retransmits lost segments over the network."},
		{ID: "c-dns", ParentID: "p-dns", Text: "DNS resolves a name to an address."},
	}
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"one content term matches", "What is TCP?", []string{"c-tcp"}},
		{"two terms must both be shared", "TCP handshake", nil},
		{"two shared terms match", "lost segments", []string{"c-tcp"}},
		{"stopwords alone do not match a topic", "what is the best pizza in town", nil},
		{"a query of only stopwords keeps them", "to a", []string{"c-dns"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idsOf(rankBM25(docs, tt.query, 5))
			if len(got) != len(tt.want) {
				t.Fatalf("rank = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("rank = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestCapPerSource(t *testing.T) {
	chunk := func(id, source string) domain.Chunk { return domain.Chunk{ID: id, SourceID: source} }
	ranked := []domain.Chunk{chunk("a1", "a"), chunk("a2", "a"), chunk("a3", "a"), chunk("b1", "b"), chunk("a4", "a"), chunk("c1", "c")}
	tests := []struct {
		name  string
		in    []domain.Chunk
		limit int
		want  string
	}{
		{"caps one source and keeps rank", ranked, 4, "a1 a2 b1 c1"},
		{"backfills when sources run out", ranked, 5, "a1 a2 b1 c1 a3"},
		{"limit wins", ranked, 2, "a1 a2"},
		{"empty", nil, 3, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(idsOf(capPerSource(tt.in, tt.limit)), " ")
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
