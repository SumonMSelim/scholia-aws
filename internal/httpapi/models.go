package httpapi

import (
	"net/http"

	"github.com/sumonmselim/scholia-aws/internal/embed"
	"github.com/sumonmselim/scholia-aws/internal/model"
)

type modelBody struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
}

// defaultsBody tells the client what answers when nothing is chosen, so a user
// with no key or model still sees a working setup.
type defaultsBody struct {
	ChatModel    string `json:"chat_model"`
	EmbedModel   string `json:"embed_model"`
	WebSearch    bool   `json:"web_search"`
	ServerModels bool   `json:"server_models"`
}

type modelListResponse struct {
	Models   []modelBody  `json:"models"`
	Defaults defaultsBody `json:"defaults"`
}

func (s *server) listModels(w http.ResponseWriter, r *http.Request) {
	out := make([]modelBody, len(model.Catalog))
	for i, entry := range model.Catalog {
		out[i] = modelBody{ID: entry.ID, Provider: entry.Provider, Label: entry.Label, Kind: entry.Kind}
	}
	writeJSON(w, http.StatusOK, modelListResponse{Models: out, Defaults: defaultsBody{
		ChatModel:    s.defaultChatModel(),
		EmbedModel:   s.defaultEmbedModel(),
		WebSearch:    s.opts.WebSearch,
		ServerModels: s.opts.ServerModels && s.opts.Switch.State(r.Context()).ServerModels,
	}})
}

func (s *server) defaultChatModel() string {
	if s.opts.DefaultChatModel != "" {
		return s.opts.DefaultChatModel
	}
	return model.DefaultModel
}

func (s *server) defaultEmbedModel() string {
	if s.opts.EmbedModel != "" {
		return s.opts.EmbedModel
	}
	return embed.DefaultModel
}
