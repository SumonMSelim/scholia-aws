package demo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

const vttBody = "WEBVTT\n\n00:00:01.000 --> 00:00:04.000\nHashing maps keys to slots.\n"

// ocw serves every lecture page, notes page and file Fetch asks for.
// skip names a path that answers 404 instead; hits counts file downloads.
func ocw(t *testing.T, skip string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == skip {
			http.NotFound(w, r)
			return
		}
		switch {
		case strings.HasSuffix(path, ".vtt"):
			hits.Add(1)
			_, _ = fmt.Fprint(w, vttBody)
		case strings.HasSuffix(path, ".pdf"):
			hits.Add(1)
			_, _ = fmt.Fprint(w, "%PDF-1.4 notes")
		case strings.Contains(path, "/resources/lecture-"):
			_, _ = fmt.Fprintf(w, `<a href="%sabc_ZA-tUyM_y7s.vtt">vtt</a><iframe src="https://www.youtube.com/embed/ZA-tUyM_y7s"></iframe>`, coursePath)
		case strings.Contains(path, "/resources/mit6_006s20_lec"):
			_, _ = fmt.Fprintf(w, `<a href="%sdef_MIT6_006S20_lec.pdf">pdf</a>`, coursePath)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fetcher(srv *httptest.Server, dir string) Fetcher {
	return Fetcher{Client: srv.Client(), OCW: srv.URL, BookURL: srv.URL + "/book/ods.pdf", Dir: dir}
}

func TestFetch(t *testing.T) {
	var hits atomic.Int32
	srv := ocw(t, "", &hits)
	dir := t.TempDir()
	files, err := fetcher(srv, dir).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	// Attribution, 19 transcripts, 18 lecture notes and the book.
	if len(files) != 1+19+18+1 {
		t.Fatalf("files = %d", len(files))
	}
	if files[0].ContentType != "text/markdown" || files[len(files)-1].Name != "Open Data Structures - Pat Morin.pdf" {
		t.Fatalf("first %+v last %+v", files[0], files[len(files)-1])
	}
	lec := files[1]
	if lec.Name != "Lecture 01 - Algorithms and Computation - transcript.vtt" || lec.ContentType != "text/vtt" || lec.YouTubeID != "ZA-tUyM_y7s" {
		t.Fatalf("lecture file %+v", lec)
	}
	if body, err := os.ReadFile(lec.Path); err != nil || string(body) != vttBody {
		t.Fatalf("transcript on disk %q err %v", body, err)
	}
	for _, f := range files {
		if f.ContentType == "application/pdf" && f.YouTubeID != "" {
			t.Fatalf("only transcripts carry a video: %+v", f)
		}
	}

	// A second run reads the pages again but downloads nothing it already has.
	before := hits.Load()
	if _, err := fetcher(srv, dir).Fetch(context.Background()); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if hits.Load() != before {
		t.Fatalf("second fetch downloaded %d files", hits.Load()-before)
	}
}

func TestFetchFailures(t *testing.T) {
	tests := []struct {
		name string
		skip string
		want string
	}{
		{"lecture page", coursePath + "resources/lecture-4-hashing/", "lecture 4"},
		{"notes page", coursePath + "resources/mit6_006s20_lec4/", "lecture 4 notes"},
		{"transcript file", coursePath + "abc_ZA-tUyM_y7s.vtt", "lecture 1 transcript"},
		{"notes file", coursePath + "def_MIT6_006S20_lec.pdf", "lecture 1 notes"},
		{"book", "/book/ods.pdf", "textbook"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits atomic.Int32
			_, err := fetcher(ocw(t, tt.skip, &hits), t.TempDir()).Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestFetchPageWithoutLinks(t *testing.T) {
	tests := []struct {
		name string
		page string
		want string
	}{
		{"lecture without video", `<a href="` + coursePath + `a.vtt">`, "no transcript or video"},
		{"notes without pdf", "<p>no file</p>", "no pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, ".vtt"):
					_, _ = fmt.Fprint(w, vttBody)
				case strings.Contains(r.URL.Path, "/resources/lecture-") && tt.want == "no pdf":
					_, _ = fmt.Fprintf(w, `<a href="%sa.vtt"></a>youtube.com/embed/ZA-tUyM_y7s`, coursePath)
				default:
					_, _ = fmt.Fprint(w, tt.page)
				}
			}))
			defer srv.Close()
			_, err := fetcher(srv, t.TempDir()).Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestFetchRejectsOversizedFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxDownload+1))
	}))
	defer srv.Close()
	_, err := fetcher(srv, t.TempDir()).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchBadDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Fetcher{Dir: filepath.Join(file, "sub")}).Fetch(context.Background()); err == nil {
		t.Fatal("want an error for a directory under a file")
	}
}

type fakeStore struct {
	course     domain.Course
	sources    map[string]domain.Source
	courseErr  error
	getErr     error
	putErr     error
	putSources []domain.Source
}

func (f *fakeStore) PutCourse(_ context.Context, c domain.Course) error {
	f.course = c
	return f.courseErr
}

func (f *fakeStore) GetSource(_ context.Context, _, id string) (domain.Source, error) {
	if f.getErr != nil {
		return domain.Source{}, f.getErr
	}
	src, ok := f.sources[id]
	if !ok {
		return domain.Source{}, store.ErrNotFound
	}
	return src, nil
}

func (f *fakeStore) PutSource(_ context.Context, src domain.Source) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.putSources = append(f.putSources, src)
	return nil
}

type fakeObjects struct {
	keys []string
	err  error
}

func (f *fakeObjects) Put(_ context.Context, bucket, key, _, tagging string, _ []byte) error {
	if f.err != nil {
		return f.err
	}
	if bucket != "uploads" || tagging != "" {
		return fmt.Errorf("bucket %q tagging %q", bucket, tagging)
	}
	f.keys = append(f.keys, key)
	return nil
}

func seedFiles(t *testing.T) []File {
	t.Helper()
	dir := t.TempDir()
	vtt := filepath.Join(dir, "lec4.vtt")
	notes := filepath.Join(dir, "lec4.pdf")
	if err := os.WriteFile(vtt, []byte(vttBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notes, []byte("%PDF-1.4"), 0o600); err != nil {
		t.Fatal(err)
	}
	return []File{
		{Path: vtt, Name: "Lecture 04 - Hashing - transcript.vtt", ContentType: "text/vtt", YouTubeID: "ZA-tUyM_y7s"},
		{Path: notes, Name: "Lecture 04 - Hashing - notes.pdf", ContentType: "application/pdf"},
	}
}

func TestSeed(t *testing.T) {
	files := seedFiles(t)
	st := &fakeStore{sources: map[string]domain.Source{
		// Already ingested: left alone. A failed one would be queued again.
		SourceID(files[1].Name): {ID: SourceID(files[1].Name), Status: domain.SourceReady},
	}}
	objects := &fakeObjects{}
	res, err := Seed(context.Background(), st, objects, "uploads", files)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if res != (Result{Queued: 1, Skipped: 1}) {
		t.Fatalf("result %+v", res)
	}
	if st.course.ID != CourseID || !st.course.Public || st.course.OwnerID != Owner || st.course.ExpiresAt != 0 {
		t.Fatalf("course %+v", st.course)
	}
	got := st.putSources[0]
	if got.ID != SourceID(files[0].Name) || got.Status != domain.SourceQueued || got.YouTubeID != "ZA-tUyM_y7s" || got.ExpiresAt != 0 {
		t.Fatalf("source %+v", got)
	}
	want := "courses/" + CourseID + "/sources/" + got.ID + "/" + files[0].Name
	if len(objects.keys) != 1 || objects.keys[0] != want {
		t.Fatalf("keys %v want %s", objects.keys, want)
	}

	st.sources[SourceID(files[0].Name)] = domain.Source{Status: domain.SourceFailed, FailureReason: "x"}
	res, err = Seed(context.Background(), st, objects, "uploads", files)
	if err != nil || res != (Result{Queued: 1, Skipped: 1}) {
		t.Fatalf("rerun %+v err %v", res, err)
	}
}

func TestSeedFailures(t *testing.T) {
	boom := errors.New("boom")
	files := seedFiles(t)
	tests := []struct {
		name    string
		bucket  string
		store   *fakeStore
		objects *fakeObjects
		files   []File
	}{
		{"no bucket", "", &fakeStore{}, &fakeObjects{}, files},
		{"course", "uploads", &fakeStore{courseErr: boom}, &fakeObjects{}, files},
		{"missing file", "uploads", &fakeStore{}, &fakeObjects{}, []File{{Path: "/nonexistent", Name: "a.pdf", ContentType: "application/pdf"}}},
		{"bad type", "uploads", &fakeStore{}, &fakeObjects{}, []File{{Path: files[0].Path, Name: "a.exe", ContentType: "application/x-msdownload"}}},
		{"lookup", "uploads", &fakeStore{getErr: boom}, &fakeObjects{}, files},
		{"put source", "uploads", &fakeStore{putErr: boom}, &fakeObjects{}, files},
		{"put object", "uploads", &fakeStore{}, &fakeObjects{err: boom}, files},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Seed(context.Background(), tt.store, tt.objects, tt.bucket, tt.files); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestSourceIDIsStable(t *testing.T) {
	a, b := SourceID("x.pdf"), SourceID("x.pdf")
	if a != b || len(a) != 32 || SourceID("y.pdf") == a {
		t.Fatalf("ids %s %s", a, b)
	}
}

func TestLectureNamesAreValidUploads(t *testing.T) {
	for _, lec := range Lectures {
		if err := ingest.ValidateUpload(lectureName(lec, "transcript.vtt"), "text/vtt", 1); err != nil {
			t.Fatalf("lecture %d: %v", lec.N, err)
		}
		if err := ingest.ValidateUpload(lectureName(lec, "notes.pdf"), "application/pdf", 1); err != nil {
			t.Fatalf("lecture %d: %v", lec.N, err)
		}
	}
}
