package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

const maxTitleRunes = 200

type courseBody struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Public bool   `json:"public"`
	Mine   bool   `json:"mine"`
}

type courseListResponse struct {
	Courses []courseBody `json:"courses"`
}

type createCourseRequest struct {
	Title  string `json:"title"`
	Public bool   `json:"public"`
}

type sourceBody struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContentType   string `json:"content_type"`
	Status        string `json:"status"`
	FailureReason string `json:"failure_reason,omitempty"`
	YouTubeID     string `json:"youtube_id,omitempty"`
}

type sourceListResponse struct {
	Sources []sourceBody `json:"sources"`
}

func (s *server) listCourses(w http.ResponseWriter, r *http.Request) {
	if s.opts.Sources == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "courses are not configured")
		return
	}
	courses, err := s.opts.Sources.ListCourses(r.Context())
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "list courses", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not list courses")
		return
	}
	who := s.caller(r)
	out := make([]courseBody, 0, len(courses))
	for _, course := range courses {
		if !who.canRead(course) {
			continue
		}
		out = append(out, courseJSON(course, who))
	}
	writeJSON(w, http.StatusOK, courseListResponse{Courses: out})
}

func (s *server) createCourse(w http.ResponseWriter, r *http.Request) {
	if s.opts.Sources == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "courses are not configured")
		return
	}
	var req createCourseRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "request body is not valid")
		return
	}
	who := s.caller(r)
	if who.enforced && who.subject == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to create a course")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || utf8.RuneCountInString(title) > maxTitleRunes {
		writeError(w, http.StatusBadRequest, "invalid", "title is required and must be at most 200 characters")
		return
	}
	newID := s.opts.NewID
	if newID == nil {
		newID = ingest.NewID
	}
	id, err := newID()
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "new id", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not create course")
		return
	}
	// A guest's course is never public: guests are anonymous, and it expires.
	course := domain.Course{ID: id, Title: title, OwnerID: who.subject, Public: req.Public && !who.guest(), ExpiresAt: s.expiry(who)}
	if err := s.opts.Sources.PutCourse(r.Context(), course); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "put course", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not create course")
		return
	}
	writeJSON(w, http.StatusCreated, courseJSON(course, who))
}

func courseJSON(course domain.Course, who caller) courseBody {
	return courseBody{ID: course.ID, Title: course.Title, Public: course.Public, Mine: owns(who, course)}
}

func owns(who caller, course domain.Course) bool {
	if !who.enforced {
		return true
	}
	return who.subject != "" && course.OwnerID == who.subject
}

func (s *server) listSources(w http.ResponseWriter, r *http.Request) {
	if s.opts.Sources == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "courses are not configured")
		return
	}
	courseID := r.PathValue("courseID")
	course, err := s.opts.Sources.GetCourse(r.Context(), courseID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "course not found")
			return
		}
		s.opts.Logger.ErrorContext(r.Context(), "get course", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not list sources")
		return
	}
	if who := s.caller(r); !who.canRead(course) {
		deny(w, who, false)
		return
	}
	sources, err := s.opts.Sources.ListSources(r.Context(), courseID)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "list sources", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not list sources")
		return
	}
	out := make([]sourceBody, len(sources))
	for i, src := range sources {
		out[i] = sourceBody{
			ID: src.ID, Name: src.Name, ContentType: src.ContentType,
			Status: string(src.Status), FailureReason: src.FailureReason, YouTubeID: src.YouTubeID,
		}
	}
	writeJSON(w, http.StatusOK, sourceListResponse{Sources: out})
}
