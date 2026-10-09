// Package answer turns a course message into a streamed reply with citations.
//
// A classifier call reads the message first. Off-topic and unsafe messages are
// refused and the main model is not called. Retrieval runs next; when the
// course material is thin the web can fill in. With nothing to answer from the
// reply is a refusal. Otherwise the system prompt stays a fixed string per
// task: the question, passages, web results and attachments go in the user
// message, each inside a fence labeled untrusted so they cannot be read as
// instructions.
package answer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/attach"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/embed"
	"github.com/sumonmselim/scholia-aws/internal/locator"
	"github.com/sumonmselim/scholia-aws/internal/model"
	"github.com/sumonmselim/scholia-aws/internal/websearch"
)

const (
	defaultLimit = 5
	// thinChunks is the passage count under which the web may fill in.
	thinChunks = 2
	webTimeout = 8 * time.Second
	// examContextRunes is how much of the last question joins an exam reply's
	// retrieval query, because an answer such as "B" retrieves nothing alone.
	examContextRunes = 600
	// excerptRunes is how much of a cited passage the source list shows.
	excerptRunes = 400
)

// Search is the course-scoped retrieval used before a model call.
type Search interface {
	Search(ctx context.Context, courseID, modelID, text string, limit int) ([]domain.Chunk, error)
}

// Citation is one parent chunk the answer drew on. Chats store the same shape.
type Citation = domain.Citation

// Request is one message to answer. Bound fills the course fields and Model.
type Request struct {
	CourseID    string
	CourseTitle string
	UserID      string
	Question    string
	ChatModel   string
	EmbedModel  string
	// Task fixes the prompt. Empty lets the classifier choose.
	Task    Task
	History []Turn
	// Files are this message's chat attachments, read at send time.
	Files []attach.File
	// Model answers this request. Nil uses the service's model.
	Model model.Provider
	// Meter is who a web search is counted against: the subject, or the address
	// of an anonymous caller. Empty skips the count.
	Meter string
}

// Outcome is what remains after the token stream.
// Refused means the main model was not called; Reason and Text say why.
type Outcome struct {
	Refused bool
	Reason  string
	// Detail says what refused, for the logs: the classifier's safety label or "guardrail".
	Detail    string
	Text      string
	Task      Task
	Citations []Citation
	Web       []domain.WebResult
}

// Service answers one message.
type Service struct {
	Search Search
	Model  model.Provider
	// Web is optional. Nil leaves web search off.
	Web websearch.Searcher
	// EmbedModel and ChatModel are the service defaults. Empty uses the package defaults.
	EmbedModel string
	ChatModel  string
	Limit      int
	// WebTimeout bounds one web search. Zero uses webTimeout.
	WebTimeout time.Duration
	// MaxTokens caps each reply. Zero uses model.DefaultMaxTokens.
	MaxTokens int
	// WebAllowed counts one web search against meter and reports whether the
	// day's allowance had room. Nil allows every search. A spent allowance
	// answers without the web; it is not an error.
	WebAllowed func(ctx context.Context, meter string) bool
}

// Answer classifies, retrieves, then streams one reply.
func (s *Service) Answer(ctx context.Context, req Request, emit func(delta string) error) (Outcome, error) {
	if s.Search == nil || s.Model == nil {
		return Outcome{}, errors.New("answer: search and model are required")
	}
	req.CourseID = strings.TrimSpace(req.CourseID)
	req.Question = strings.TrimSpace(req.Question)
	if req.CourseID == "" || req.Question == "" {
		return Outcome{}, errors.New("answer: course id and question are required")
	}
	provider := req.Model
	if provider == nil {
		provider = s.Model
	}
	chatModel := firstNonEmpty(req.ChatModel, s.ChatModel, model.DefaultModel)
	embedModel := firstNonEmpty(req.EmbedModel, s.EmbedModel, embed.DefaultModel)
	history := TrimHistory(req.History)

	verdict := classify(ctx, provider, chatModel, req.CourseTitle, history, req.Question)
	switch verdict.Intent {
	case intentUnsafe:
		return Outcome{Refused: true, Reason: domain.RefusalUnsafe, Detail: verdict.Safety, Text: unsafeText(verdict.Safety, req.CourseTitle)}, nil
	case intentOffTopic:
		return Outcome{Refused: true, Reason: domain.RefusalOffTopic, Text: offTopicText(req.CourseTitle)}, nil
	}
	task := req.Task
	if task == "" {
		task = Task(verdict.Intent)
	}

	files := readFiles(ctx, req.Files, provider)
	limit := s.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	chunks, err := s.Search.Search(ctx, req.CourseID, embedModel, retrievalQuery(task, history, req.Question), limit)
	if err != nil {
		return Outcome{}, err
	}
	chunks = usable(chunks)
	var web []websearch.Result
	if s.Web != nil && len(chunks) < thinChunks && webTask(task) && (s.WebAllowed == nil || s.WebAllowed(ctx, req.Meter)) {
		web = s.searchWeb(ctx, req.Question)
	}
	examGoing := task == TaskExam && len(history) > 0
	if len(chunks) == 0 && len(web) == 0 && len(files) == 0 && !examGoing {
		return Outcome{Refused: true, Reason: domain.RefusalNoMaterial, Text: RefusalText, Task: task}, nil
	}

	msgs := conversation(systemPrompt(task), history, userMessage(req.CourseTitle, chunks, web, files), req.Question)
	if err := provider.Stream(ctx, model.Request{Model: chatModel, Messages: msgs, MaxTokens: s.MaxTokens}, emit); err != nil {
		if errors.Is(err, model.ErrGuardrail) {
			return Outcome{Refused: true, Reason: domain.RefusalUnsafe, Detail: "guardrail", Text: unsafeText(safetyHarmful, req.CourseTitle), Task: task}, nil
		}
		return Outcome{}, err
	}
	return Outcome{Task: task, Citations: citations(chunks), Web: webResults(web)}, nil
}

// webTask reports whether the web may fill in for thin material. Reviews and
// exams are about the course's own material, so they never go to the web.
func webTask(task Task) bool {
	return task == TaskQuestion || task == TaskSolve || task == TaskExplain
}

// searchWeb sends the question alone. The web only runs when the course lacks
// the topic, and a course title in the query pulled back copies of the course
// itself instead of pages on the question. A failure answers without the web.
func (s *Service) searchWeb(ctx context.Context, question string) []websearch.Result {
	timeout := s.WebTimeout
	if timeout == 0 {
		timeout = webTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	results, err := s.Web.Search(ctx, question)
	if err != nil {
		return nil
	}
	return results
}

func retrievalQuery(task Task, history []Turn, question string) string {
	if task != TaskExam {
		return question
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == domain.RoleAssistant && strings.TrimSpace(history[i].Text) != "" {
			return clipRunes(history[i].Text, examContextRunes) + "\n" + question
		}
	}
	return question
}

// readFile is one attachment after reading. Err is set when it could not be read,
// and the model is told so rather than the whole message failing.
type readFile struct {
	name string
	text string
	err  error
}

func readFiles(ctx context.Context, files []attach.File, reader attach.Reader) []readFile {
	out := make([]readFile, 0, len(files))
	for _, f := range files {
		text, err := attach.Convert(ctx, f, reader)
		out = append(out, readFile{name: f.Name, text: text, err: err})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func usable(chunks []domain.Chunk) []domain.Chunk {
	out := make([]domain.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.Text) == "" {
			continue
		}
		out = append(out, chunk)
	}
	return out
}

// userMessage holds everything that changes per turn. The student's text is
// last so the fences read as context for it.
func userMessage(course string, chunks []domain.Chunk, web []websearch.Result, files []readFile) string {
	var b strings.Builder
	if course != "" {
		b.WriteString(fence("course", []string{course}) + "\n\n")
	}
	if len(chunks) > 0 {
		passages := make([]string, len(chunks))
		for i, chunk := range chunks {
			passages[i] = chunk.Text
		}
		b.WriteString(fence(sourceRetrieval, passages) + "\n\n")
	} else {
		b.WriteString("No course passage matched this message.\n\n")
	}
	if len(web) > 0 {
		items := make([]string, len(web))
		for i, row := range web {
			items[i] = row.Title + "\n" + row.URL + "\n" + row.Snippet
		}
		b.WriteString(fence(sourceWeb, items) + "\n\n")
	}
	for _, f := range files {
		body := f.text
		if f.err != nil {
			body = fmt.Sprintf("(this file could not be read: %v)", f.err)
		}
		b.WriteString(fence(sourceAttachment, []string{"File: " + f.name + "\n" + body}) + "\n\n")
	}
	// conversation adds the question itself as the guarded part of this message.
	b.WriteString("Student message:")
	return b.String()
}

func citations(chunks []domain.Chunk) []Citation {
	out := make([]Citation, len(chunks))
	for i, chunk := range chunks {
		locs := chunk.Locators
		if locs == nil {
			locs = []locator.Locator{}
		}
		out[i] = Citation{
			ChunkID: chunk.ID, SourceID: chunk.SourceID, Locators: locs,
			Section: chunk.Breadcrumb, Excerpt: clipRunes(strings.TrimSpace(chunk.Text), excerptRunes),
		}
	}
	return out
}

func webResults(results []websearch.Result) []domain.WebResult {
	out := make([]domain.WebResult, len(results))
	for i, row := range results {
		out[i] = domain.WebResult{Title: row.Title, URL: row.URL}
	}
	return out
}
