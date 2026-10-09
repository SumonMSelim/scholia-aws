// Package demo seeds the public demo course every visitor can open without
// uploading anything: MIT OpenCourseWare 6.006 Introduction to Algorithms
// (Spring 2020) with the Open Data Structures textbook.
//
// Fetch downloads the openly licensed files into a local directory. Seed writes
// the course and its source records, then puts each file in the uploads bucket,
// where the worker ingests it like any other upload.
package demo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

// The course is public and owned by a subject no sign-in can produce, so every
// visitor can read and ask it and nobody can change it.
const (
	CourseID = "mit-6006-spring-2020"
	Title    = "6.006 Introduction to Algorithms (MIT OCW, Spring 2020)"
	Owner    = "scholia-demo"
)

// Default origins. Tests point Fetcher at a local server instead.
const (
	OCWOrigin = "https://ocw.mit.edu"
	BookURL   = "https://opendatastructures.org/ods-python.pdf"
)

const coursePath = "/courses/6-006-introduction-to-algorithms-spring-2020/"

// maxDownload bounds one file. The largest, the textbook, is about 1.5 MB.
const maxDownload = 32 << 20

// Lecture is one 6.006 lecture. Resource is its OCW page; the lecture notes
// live on their own page, which every lecture but 18 has.
type Lecture struct {
	N        int
	Title    string
	Resource string
	NoNotes  bool
}

// Lectures is the Spring 2020 lecture list as OCW publishes it.
var Lectures = []Lecture{
	{1, "Algorithms and Computation", "lecture-1-algorithms-and-computation", false},
	{2, "Data Structures and Dynamic Arrays", "lecture-2-data-structures-and-dynamic-arrays", false},
	{3, "Sets and Sorting", "lecture-3-sets-and-sorting", false},
	{4, "Hashing", "lecture-4-hashing", false},
	{5, "Linear Sorting", "lecture-5-linear-sorting", false},
	{6, "Binary Trees, Part 1", "lecture-6-binary-trees-part-1", false},
	{7, "Binary Trees, Part 2 - AVL", "lecture-7-binary-trees-part-2-avl", false},
	{8, "Binary Heaps", "lecture-8-binary-heaps", false},
	{9, "Breadth-First Search", "lecture-9-breadth-first-search", false},
	{10, "Depth-First Search", "lecture-10-depth-first-search", false},
	{11, "Weighted Shortest Paths", "lecture-11-weighted-shortest-paths", false},
	{12, "Bellman-Ford", "lecture-12-bellman-ford", false},
	{13, "Dijkstra", "lecture-13-dijkstra", false},
	{14, "APSP and Johnson", "lecture-14-apsp-and-johnson", false},
	{15, "Dynamic Programming, Part 1", "lecture-15-dynamic-programming-part-1-srtbot-fib-dags-bowling", false},
	{16, "Dynamic Programming, Part 2", "lecture-16-dynamic-programming-part-2-lcs-lis-coins", false},
	{17, "Dynamic Programming, Part 3", "lecture-17-dynamic-programming-part-3-apsp-parens-piano", false},
	{18, "Dynamic Programming, Part 4", "lecture-18-dynamic-programming-part-4-rods-subset-sum-pseudopolynomial", true},
	{19, "Complexity", "lecture-19-complexity", false},
}

// Attribution is what both licenses ask for. It is seeded as a course
// file, so it shows in the course's file list and the assistant can answer
// where the material comes from.
const Attribution = `# About this demo course

This is a demo course in Scholia, built only from openly licensed material. Scholia is a free, open-source project and is not affiliated with or endorsed by MIT or the authors below.

## MIT 6.006 Introduction to Algorithms, Spring 2020

Erik Demaine, Jason Ku, and Justin Solomon. 6.006 Introduction to Algorithms. Spring 2020. Massachusetts Institute of Technology: MIT OpenCourseWare, https://ocw.mit.edu/courses/6-006-introduction-to-algorithms-spring-2020/. License: Creative Commons BY-NC-SA 4.0, https://creativecommons.org/licenses/by-nc-sa/4.0/.

The lecture transcripts and lecture notes are split into passages for search; their text is not changed. Lecture videos play from MIT OpenCourseWare's YouTube channel.

## Open Data Structures

Pat Morin. Open Data Structures (pseudocode edition). https://opendatastructures.org/. License: Creative Commons Attribution 2.5 Canada, https://creativecommons.org/licenses/by/2.5/ca/.
`

// File is one course file on local disk and the source record it becomes.
type File struct {
	Path        string
	Name        string
	ContentType string
	YouTubeID   string
}

// Fetcher downloads the course files into Dir. A file already in Dir is not
// downloaded again; lecture pages are always read, since they hold the video id.
type Fetcher struct {
	Client  *http.Client
	OCW     string
	BookURL string
	Dir     string
}

var (
	vttLink = regexp.MustCompile(regexp.QuoteMeta(coursePath) + `[^"'\s]*\.vtt`)
	pdfLink = regexp.MustCompile(regexp.QuoteMeta(coursePath) + `[^"'\s]*\.pdf`)
	ytEmbed = regexp.MustCompile(`youtube(?:-nocookie)?\.com/embed/([A-Za-z0-9_-]{11})`)
)

// Fetch returns every course file, ready to seed. A lecture page without a
// transcript or a video is an error: the demo depends on both.
func (f Fetcher) Fetch(ctx context.Context) ([]File, error) {
	if err := os.MkdirAll(f.Dir, 0o750); err != nil {
		return nil, err
	}
	credits := filepath.Join(f.Dir, "attribution.md")
	if err := os.WriteFile(credits, []byte(Attribution), 0o600); err != nil {
		return nil, err
	}
	files := []File{{Path: credits, Name: "About this course and attribution.md", ContentType: "text/markdown"}}

	for _, lec := range Lectures {
		page, err := f.get(ctx, f.OCW+coursePath+"resources/"+lec.Resource+"/")
		if err != nil {
			return nil, fmt.Errorf("lecture %d: %w", lec.N, err)
		}
		vtt := vttLink.Find(page)
		video := ytEmbed.FindSubmatch(page)
		if vtt == nil || video == nil {
			return nil, fmt.Errorf("lecture %d: page has no transcript or video", lec.N)
		}
		path := filepath.Join(f.Dir, "lec"+strconv.Itoa(lec.N)+".vtt")
		if err := f.download(ctx, f.OCW+string(vtt), path); err != nil {
			return nil, fmt.Errorf("lecture %d transcript: %w", lec.N, err)
		}
		files = append(files, File{
			Path: path, Name: lectureName(lec, "transcript.vtt"), ContentType: "text/vtt", YouTubeID: string(video[1]),
		})
		if lec.NoNotes {
			continue
		}
		notes, err := f.get(ctx, f.OCW+coursePath+"resources/mit6_006s20_lec"+strconv.Itoa(lec.N)+"/")
		if err != nil {
			return nil, fmt.Errorf("lecture %d notes: %w", lec.N, err)
		}
		pdf := pdfLink.Find(notes)
		if pdf == nil {
			return nil, fmt.Errorf("lecture %d notes: page has no pdf", lec.N)
		}
		path = filepath.Join(f.Dir, "lec"+strconv.Itoa(lec.N)+".pdf")
		if err := f.download(ctx, f.OCW+string(pdf), path); err != nil {
			return nil, fmt.Errorf("lecture %d notes: %w", lec.N, err)
		}
		files = append(files, File{Path: path, Name: lectureName(lec, "notes.pdf"), ContentType: "application/pdf"})
	}

	book := filepath.Join(f.Dir, "ods.pdf")
	if err := f.download(ctx, f.BookURL, book); err != nil {
		return nil, fmt.Errorf("textbook: %w", err)
	}
	files = append(files, File{Path: book, Name: "Open Data Structures - Pat Morin.pdf", ContentType: "application/pdf"})
	return files, nil
}

func lectureName(lec Lecture, suffix string) string {
	return fmt.Sprintf("Lecture %02d - %s - %s", lec.N, lec.Title, suffix)
}

func (f Fetcher) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDownload {
		return nil, fmt.Errorf("GET %s: larger than %d bytes", url, maxDownload)
	}
	return body, nil
}

func (f Fetcher) download(ctx context.Context, url, path string) error {
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return nil
	}
	body, err := f.get(ctx, url)
	if err != nil {
		return err
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Store is the part of the repository Seed writes. *store.Repository satisfies it.
type Store interface {
	PutCourse(ctx context.Context, course domain.Course) error
	GetSource(ctx context.Context, courseID, sourceID string) (domain.Source, error)
	PutSource(ctx context.Context, source domain.Source) error
}

// Objects puts a file in the uploads bucket. *ingest.Bucket satisfies it.
type Objects interface {
	Put(ctx context.Context, bucket, key, contentType, tagging string, body []byte) error
}

// Result counts what one Seed run did.
type Result struct {
	Queued  int
	Skipped int
}

// SourceID is stable per file name, so running Seed again finds the sources it
// already made instead of adding copies.
func SourceID(name string) string {
	sum := sha256.Sum256([]byte(CourseID + "/" + name))
	return hex.EncodeToString(sum[:16])
}

// Seed writes the course, then queues each file that is not already queued,
// processing or ready. A failed file is queued again.
func Seed(ctx context.Context, st Store, objects Objects, bucket string, files []File) (Result, error) {
	var res Result
	if bucket == "" {
		return res, errors.New("demo: uploads bucket is required")
	}
	course := domain.Course{ID: CourseID, Title: Title, OwnerID: Owner, Public: true}
	if err := st.PutCourse(ctx, course); err != nil {
		return res, err
	}
	for _, file := range files {
		body, err := os.ReadFile(file.Path)
		if err != nil {
			return res, err
		}
		if err := ingest.ValidateUpload(file.Name, file.ContentType, int64(len(body))); err != nil {
			return res, fmt.Errorf("%s: %w", file.Name, err)
		}
		id := SourceID(file.Name)
		existing, err := st.GetSource(ctx, CourseID, id)
		switch {
		case err == nil && existing.Status != domain.SourceFailed:
			res.Skipped++
			continue
		case err != nil && !errors.Is(err, store.ErrNotFound):
			return res, err
		}
		src := domain.Source{
			ID: id, CourseID: CourseID, Name: file.Name, ContentType: file.ContentType,
			Status: domain.SourceQueued, YouTubeID: file.YouTubeID,
		}
		// The record comes first: the worker looks it up when the object event arrives.
		if err := st.PutSource(ctx, src); err != nil {
			return res, err
		}
		if err := objects.Put(ctx, bucket, ingest.ObjectKey(CourseID, id, file.Name), file.ContentType, "", body); err != nil {
			return res, fmt.Errorf("%s: %w", file.Name, err)
		}
		res.Queued++
	}
	return res, nil
}
