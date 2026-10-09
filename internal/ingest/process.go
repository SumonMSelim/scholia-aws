package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"bytes"
	"image"
	_ "image/png"

	"github.com/sumonmselim/scholia-aws/internal/chunk"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/embed"
	"github.com/sumonmselim/scholia-aws/internal/locator"
	"github.com/sumonmselim/scholia-aws/internal/pdfdoc"
	"github.com/sumonmselim/scholia-aws/internal/pptx"
	"github.com/sumonmselim/scholia-aws/internal/store"
	"github.com/sumonmselim/scholia-aws/internal/transcript"
	"github.com/sumonmselim/scholia-aws/internal/vector"
	"github.com/sumonmselim/scholia-aws/internal/vision"
)

// Store is the source record the worker updates. *store.Repository satisfies it.
type Store interface {
	GetSource(ctx context.Context, courseID, sourceID string) (domain.Source, error)
	PutSource(ctx context.Context, source domain.Source) error
	PutChunk(ctx context.Context, chunk domain.Chunk) error
}

// Objects reads an uploaded object and stores derived output.
type Objects interface {
	Stat(ctx context.Context, bucket, key string) (int64, error)
	Prefix(ctx context.Context, bucket, key string, n int) ([]byte, error)
	// Put writes a derived object. tagging is the S3 tag set, empty for none.
	Put(ctx context.Context, bucket, key, contentType, tagging string, body []byte) error
	Get(ctx context.Context, bucket, key string) ([]byte, error)
	// Delete removes an object the worker refuses, so it does not sit in the bucket.
	Delete(ctx context.Context, bucket, key string) error
}

// Processor moves an uploaded object from queued to ready or failed.
type Processor struct {
	Store         Store
	Objects       Objects
	Vision        vision.Model
	UploadsBucket string
	// Embed and Vectors make new chunks searchable by meaning. Either nil leaves
	// retrieval on keywords alone. EmbedModel names the index; empty is the default.
	Embed      embed.Embedder
	Vectors    vector.Store
	EmbedModel string
	// Log reports an embedding failure. Nil discards it.
	Log *slog.Logger
}

type s3Event struct {
	Records []struct {
		S3 struct {
			Bucket struct {
				Name string `json:"name"`
			} `json:"bucket"`
			Object struct {
				Key string `json:"key"`
			} `json:"object"`
		} `json:"s3"`
	} `json:"Records"`
}

// HandleMessage accepts one SQS body: an S3 upload notification.
func (p *Processor) HandleMessage(ctx context.Context, body string) error {
	var test struct {
		Event string `json:"Event"`
	}
	if err := json.Unmarshal([]byte(body), &test); err != nil {
		return fmt.Errorf("upload event: %w", err)
	}
	// S3 sends one test event when the bucket notification is set up. Acknowledging
	// it keeps it out of the dead-letter queue.
	if test.Event == "s3:TestEvent" {
		return nil
	}
	var event s3Event
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		return fmt.Errorf("upload event: %w", err)
	}
	if len(event.Records) == 0 {
		return errors.New("upload event has no records")
	}
	for _, rec := range event.Records {
		key, err := url.QueryUnescape(rec.S3.Object.Key)
		if err != nil {
			return fmt.Errorf("upload key: %w", err)
		}
		if err := p.HandleUpload(ctx, rec.S3.Bucket.Name, key); err != nil {
			return err
		}
	}
	return nil
}

// HandleUpload checks size and magic bytes, then records ready or failed.
// A recorded failure is not retried. A storage error is returned so the queue can retry.
func (p *Processor) HandleUpload(ctx context.Context, bucket, key string) error {
	courseID, sourceID, _, err := ParseObjectKey(key)
	if err != nil {
		return nil
	}
	src, err := p.Store.GetSource(ctx, courseID, sourceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("source %s: %w", sourceID, err)
		}
		return err
	}
	if src.Status == domain.SourceReady || src.Status == domain.SourceFailed {
		return nil
	}

	size, err := p.Objects.Stat(ctx, bucket, key)
	if err != nil {
		return err
	}
	if size < 1 || size > MaxUploadBytes {
		// The object is never read, so keeping it would only store what was refused.
		if err := p.Objects.Delete(ctx, bucket, key); err != nil {
			p.warn(ctx, "delete refused upload", src, err)
		}
		return p.failErr(ctx, src, reasonSize, fmt.Errorf("object size %d is outside 1..%d", size, MaxUploadBytes))
	}

	src.Status = domain.SourceProcessing
	src.FailureReason = ""
	if err := p.Store.PutSource(ctx, src); err != nil {
		return err
	}

	prefix, err := p.Objects.Prefix(ctx, bucket, key, prefixLen)
	if err != nil {
		return err
	}
	if err := CheckMagic(src.ContentType, prefix); err != nil {
		return p.failErr(ctx, src, reasonMismatch, err)
	}
	if kind, ok := transcriptKind(src.ContentType); ok {
		return p.importTranscript(ctx, src, bucket, key, kind, size)
	}
	if canonicalType(src.ContentType) == "application/pdf" {
		return p.extractPDF(ctx, src, bucket, key, size)
	}
	if canonicalType(src.ContentType) == "application/vnd.openxmlformats-officedocument.presentationml.presentation" {
		return p.extractPPTX(ctx, src, bucket, key, size)
	}
	if isText(src.ContentType) {
		return p.extractText(ctx, src, bucket, key, size)
	}
	src.Status = domain.SourceReady
	src.FailureReason = ""
	return p.Store.PutSource(ctx, src)
}

func transcriptKind(contentType string) (string, bool) {
	switch canonicalType(contentType) {
	case "text/vtt":
		return "vtt", true
	case "application/x-subrip":
		return "srt", true
	case "application/json":
		return "json", true
	default:
		return "", false
	}
}

// importTranscript stores sentences from a pre-transcribed file and marks the source ready.
// A malformed file is a recorded failure. A storage error is returned so the queue can retry.
func (p *Processor) importTranscript(ctx context.Context, src domain.Source, bucket, key, kind string, size int64) error {
	if size > maxImportBytes {
		return p.fail(ctx, src, "transcript is too large")
	}
	body, err := p.Objects.Get(ctx, bucket, key)
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return p.fail(ctx, src, "transcript is too large")
		}
		return err
	}
	sentences, err := transcript.Import(kind, body)
	if err != nil {
		return p.failErr(ctx, src, reasonTranscript, err)
	}
	return p.storeSentences(ctx, src, sentences)
}

// extractPDF stores page spans and renders scanned pages. A malformed file is a
// recorded failure. A storage error is returned so the queue can retry.
// The read cap matches transcripts: a PDF larger than that is failed before it
// is pulled into the worker.
func (p *Processor) extractPDF(ctx context.Context, src domain.Source, bucket, key string, size int64) error {
	if size > maxImportBytes {
		return p.fail(ctx, src, "pdf is too large")
	}
	body, err := p.Objects.Get(ctx, bucket, key)
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return p.fail(ctx, src, "pdf is too large")
		}
		return err
	}
	doc, err := pdfdoc.Extract(ctx, body)
	if errors.Is(err, pdfdoc.ErrTooManyPages) {
		return p.failErr(ctx, src, reasonPages, err)
	}
	if err != nil {
		return p.failErr(ctx, src, reasonPDF, err)
	}
	raw, err := pdfdoc.Encode(doc)
	if err != nil {
		return p.failErr(ctx, src, reasonPDF, err)
	}
	if p.UploadsBucket == "" {
		return errors.New("uploads bucket is required")
	}
	if err := p.Objects.Put(ctx, p.UploadsBucket, pdfdoc.ObjectKey(src.CourseID, src.ID), "application/json", derivedTag(src), raw); err != nil {
		return err
	}
	var pictures []vision.Picture
	for _, page := range doc.Pages {
		if len(page.PNG) == 0 {
			continue
		}
		if err := p.Objects.Put(ctx, p.UploadsBucket, pdfdoc.RenderKey(src.CourseID, src.ID, page.Number), "image/png", derivedTag(src), page.PNG); err != nil {
			return err
		}
		pic, err := pagePicture(src, page)
		if err != nil {
			return p.failErr(ctx, src, reasonPDF, err)
		}
		pictures = append(pictures, pic)
	}
	if err := p.readPictures(ctx, pictures); err != nil {
		return err
	}
	if err := p.putChunks(ctx, src, blocksFromPDF(doc)); err != nil {
		return err
	}
	src.Status = domain.SourceReady
	src.FailureReason = ""
	return p.Store.PutSource(ctx, src)
}

// extractPPTX stores slide text, notes, and embedded images. A malformed file
// is a recorded failure. A storage error is returned so the queue can retry.
func (p *Processor) extractPPTX(ctx context.Context, src domain.Source, bucket, key string, size int64) error {
	if size > maxImportBytes {
		return p.fail(ctx, src, "pptx is too large")
	}
	body, err := p.Objects.Get(ctx, bucket, key)
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return p.fail(ctx, src, "pptx is too large")
		}
		return err
	}
	doc, err := pptx.Extract(body)
	if err != nil {
		return p.failErr(ctx, src, reasonPPTX, err)
	}
	raw, err := pptx.Encode(doc)
	if err != nil {
		return p.failErr(ctx, src, reasonPPTX, err)
	}
	if p.UploadsBucket == "" {
		return errors.New("uploads bucket is required")
	}
	if err := p.Objects.Put(ctx, p.UploadsBucket, pptx.ObjectKey(src.CourseID, src.ID), "application/json", derivedTag(src), raw); err != nil {
		return err
	}
	var pictures []vision.Picture
	for _, slide := range doc.Slides {
		for _, img := range slide.Images {
			if len(img.Bytes) == 0 {
				continue
			}
			if err := p.Objects.Put(ctx, p.UploadsBucket, pptx.ImageKey(src.CourseID, src.ID, slide.Number, img.Name), img.ContentType, derivedTag(src), img.Bytes); err != nil {
				return err
			}
			pictures = append(pictures, vision.Picture{
				CourseID: src.CourseID, SourceID: src.ID,
				ContentType: img.ContentType, Image: img.Bytes,
				Locator: locator.Locator{Kind: locator.KindSlide, Slide: slide.Number},
			})
		}
	}
	if err := p.readPictures(ctx, pictures); err != nil {
		return err
	}
	if err := p.putChunks(ctx, src, blocksFromPPTX(doc)); err != nil {
		return err
	}
	src.Status = domain.SourceReady
	src.FailureReason = ""
	return p.Store.PutSource(ctx, src)
}

// readPictures stores an OCR child when the model says the image is a text
// block or a data visual. No model means the image stays a picture. A model
// error is returned so the queue can retry.
func (p *Processor) readPictures(ctx context.Context, pictures []vision.Picture) error {
	if p.Vision == nil || len(pictures) == 0 {
		return nil
	}
	for i := range pictures {
		parentID, err := NewID()
		if err != nil {
			return err
		}
		childID, err := NewID()
		if err != nil {
			return err
		}
		pictures[i].ParentID = parentID
		pictures[i].ChildID = childID
		parent, child, ok, err := vision.Interpret(ctx, p.Vision, pictures[i])
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := p.Store.PutChunk(ctx, parent); err != nil {
			return err
		}
		if err := p.Store.PutChunk(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

func pagePicture(src domain.Source, page pdfdoc.Page) (vision.Picture, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(page.PNG))
	if err != nil {
		return vision.Picture{}, err
	}
	box := locator.BBox{X0: 0, Y0: 0, X1: float64(cfg.Width), Y1: float64(cfg.Height)}
	return vision.Picture{
		CourseID: src.CourseID, SourceID: src.ID,
		ContentType: "image/png", Image: page.PNG,
		Locator: locator.Locator{Kind: locator.KindPage, Page: page.Number, BBox: &box},
	}, nil
}

func (p *Processor) storeSentences(ctx context.Context, src domain.Source, sentences []transcript.Sentence) error {
	raw, err := transcript.Encode(sentences)
	if err != nil {
		return p.failErr(ctx, src, reasonTranscript, err)
	}
	if p.UploadsBucket == "" {
		return errors.New("uploads bucket is required")
	}
	if err := p.Objects.Put(ctx, p.UploadsBucket, transcript.ObjectKey(src.CourseID, src.ID), "application/json", derivedTag(src), raw); err != nil {
		return err
	}
	if err := p.putChunks(ctx, src, blocksFromSentences(sentences)); err != nil {
		return err
	}
	src.Status = domain.SourceReady
	src.FailureReason = ""
	return p.Store.PutSource(ctx, src)
}

func (p *Processor) putChunks(ctx context.Context, src domain.Source, blocks []chunk.Block) error {
	units, err := chunk.Split(blocks, chunk.DefaultLimits())
	if err != nil {
		return err
	}
	items := make([]domain.Chunk, 0, len(units))
	ids := make([]string, len(units))
	for i := range units {
		id, err := NewID()
		if err != nil {
			return err
		}
		ids[i] = id
	}
	for i, unit := range units {
		item := domain.Chunk{
			ID: ids[i], CourseID: src.CourseID, SourceID: src.ID,
			Text: unit.Text, Breadcrumb: unit.Breadcrumb, Locators: unit.Locators,
			// A guest source's chunks expire with it.
			ExpiresAt: src.ExpiresAt,
		}
		if unit.Parent >= 0 {
			item.ParentID = ids[unit.Parent]
		}
		if err := p.Store.PutChunk(ctx, item); err != nil {
			return err
		}
		items = append(items, item)
	}
	p.index(ctx, src, items)
	return nil
}

// index embeds the chunks and writes them to the vector store. A failure is
// logged and not returned: the chunks are already stored and keyword search
// still finds them, while a retry would store every chunk a second time.
func (p *Processor) index(ctx context.Context, src domain.Source, chunks []domain.Chunk) {
	if p.Embed == nil || p.Vectors == nil || len(chunks) == 0 {
		return
	}
	modelID := p.EmbedModel
	if modelID == "" {
		modelID = embed.DefaultModel
	}
	vectors := make([]vector.Vector, 0, len(chunks))
	for _, c := range chunks {
		text := strings.TrimSpace(c.Breadcrumb + "\n\n" + c.Text)
		if text == "" {
			continue
		}
		values, err := p.Embed.Embed(ctx, modelID, text)
		if err != nil {
			p.warn(ctx, "embed chunks", src, err)
			return
		}
		vectors = append(vectors, vector.Vector{
			CourseID: c.CourseID, ChunkID: c.ID, ParentID: c.ParentID, Locators: c.Locators, Values: values,
		})
	}
	if err := p.Vectors.Put(ctx, modelID, vectors); err != nil {
		p.warn(ctx, "put vectors", src, err)
	}
}

func (p *Processor) warn(ctx context.Context, op string, src domain.Source, err error) {
	if p.Log == nil {
		return
	}
	p.Log.WarnContext(ctx, op, slog.String("course_id", src.CourseID), slog.String("source_id", src.ID), slog.String("err", err.Error()))
}

func blocksFromSentences(sentences []transcript.Sentence) []chunk.Block {
	out := make([]chunk.Block, 0, len(sentences))
	for _, sentence := range sentences {
		out = append(out, chunk.Block{Kind: chunk.KindParagraph, Text: sentence.Text, Locators: []locator.Locator{sentence.Locator}})
	}
	return out
}

func blocksFromPDF(doc pdfdoc.Document) []chunk.Block {
	var out []chunk.Block
	for _, page := range doc.Pages {
		for _, span := range page.Spans {
			out = append(out, chunk.Block{Kind: chunk.KindParagraph, Text: span.Text, Locators: []locator.Locator{span.Locator}})
		}
	}
	return out
}

func blocksFromPPTX(doc pptx.Document) []chunk.Block {
	var out []chunk.Block
	for _, slide := range doc.Slides {
		for _, span := range slide.Spans {
			out = append(out, chunk.Block{Kind: chunk.KindParagraph, Text: span.Text, Locators: []locator.Locator{span.Locator}})
		}
	}
	return out
}

// Failure reasons are fixed text. Any reader of a public course sees them, so a
// parser's or a service's own error, which can name internals, is logged instead.
const (
	reasonSize       = "the file is empty or larger than 1 GiB"
	reasonMismatch   = "file content does not match content type"
	reasonTranscript = "the transcript could not be read"
	reasonPDF        = "could not read this PDF"
	reasonPages      = "the PDF has too many pages"
	reasonPPTX       = "could not read this presentation"
)

// failErr records reason and logs the cause behind it.
func (p *Processor) failErr(ctx context.Context, src domain.Source, reason string, cause error) error {
	p.warn(ctx, "source failed", src, cause)
	return p.fail(ctx, src, reason)
}

func (p *Processor) fail(ctx context.Context, src domain.Source, reason string) error {
	src.Status = domain.SourceFailed
	src.FailureReason = reason
	if err := p.Store.PutSource(ctx, src); err != nil {
		return err
	}
	return nil
}

// GuestTag is the S3 tag on a guest's objects. The bucket lifecycle expires it.
const GuestTag = "guest=true"

// derivedTag tags what the worker writes from a guest's upload, so renders and
// extracted JSON expire with the upload they came from.
func derivedTag(src domain.Source) string {
	if src.ExpiresAt > 0 {
		return GuestTag
	}
	return ""
}
