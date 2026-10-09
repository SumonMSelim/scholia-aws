package answer

import (
	"context"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/model"
)

// Courses supplies a course's title.
type Courses interface {
	GetCourse(ctx context.Context, id string) (domain.Course, error)
}

// Providers builds the chat client for a request and says which model it answers
// with: the user's stored key, or a server-paid client, which may substitute the
// deployment default. A nil provider with a nil error means neither serves, and
// the service's own model answers. userID is empty for an anonymous caller.
type Providers interface {
	For(ctx context.Context, userID, modelID string) (model.Provider, string, error)
}

// Preferences reads a user's saved default model.
type Preferences interface {
	GetSettings(ctx context.Context, userID string) (domain.Settings, error)
}

// Bound answers with the course's title and the caller's key.
//
// Defaults, first match wins. This is the one place they are decided:
//
//	chat model  request or chat model → user default_model
//	            → SCHOLIA_DEFAULT_MODEL (Service.ChatModel) → model.DefaultModel
//	connection  user key for the model's provider → server Bedrock for a Bedrock
//	            model → server Bedrock with SCHOLIA_DEFAULT_MODEL for any other
//	            model → the service's own model (the mock), only with server Bedrock off
//	embeddings  SCHOLIA_EMBED_MODEL (Service.EmbedModel), always on the server's
//	            Bedrock (the fake locally), never a user key
type Bound struct {
	Service     *Service
	Courses     Courses
	Providers   Providers
	Preferences Preferences
}

// Answer fills the course title, picks the model and provider, then runs the service.
func (b Bound) Answer(ctx context.Context, req Request, emit func(delta string) error) (Outcome, error) {
	req.EmbedModel = b.Service.EmbedModel
	if b.Courses != nil {
		if course, err := b.Courses.GetCourse(ctx, req.CourseID); err == nil {
			req.CourseTitle = course.Title
		}
	}
	userDefault := ""
	if b.Preferences != nil && req.UserID != "" && req.ChatModel == "" {
		// A failed read only loses the preference; the deployment default still answers.
		if settings, err := b.Preferences.GetSettings(ctx, req.UserID); err == nil {
			userDefault = settings.DefaultModel
		}
	}
	req.ChatModel = firstNonEmpty(req.ChatModel, userDefault, b.Service.ChatModel, model.DefaultModel)
	if b.Providers != nil && req.Model == nil {
		p, used, err := b.Providers.For(ctx, req.UserID, req.ChatModel)
		if err != nil {
			return Outcome{}, err
		}
		req.Model = p
		req.ChatModel = firstNonEmpty(used, req.ChatModel)
	}
	return b.Service.Answer(ctx, req, emit)
}

// TutorMode serves the tutor through Answer so the HTTP stream can stay one handler.
type TutorMode struct {
	Bound
}

// Answer explains the concept and does not write the assignment solution.
func (t TutorMode) Answer(ctx context.Context, req Request, emit func(delta string) error) (Outcome, error) {
	req.Task = TaskExplain
	return t.Bound.Answer(ctx, req, emit)
}
