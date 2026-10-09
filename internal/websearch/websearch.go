// Package websearch looks up pages on the public web for an answer that the
// course material does not cover. Results are untrusted data: callers fence
// them in the prompt and cite them separately from course passages.
package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// TavilyURL is the Tavily search endpoint.
	TavilyURL = "https://api.tavily.com/search"
	// maxQueryRunes keeps the query under Tavily's length limit.
	maxQueryRunes = 380
	maxResults    = 5
	// maxSnippetRunes caps each result so a page cannot flood the prompt.
	maxSnippetRunes = 1200
	maxTitleRunes   = 200
	maxBodyBytes    = 1 << 20
	defaultTimeout  = 8 * time.Second
)

// Result is one page, with the text the search engine extracted from it.
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// Searcher runs one web search.
type Searcher interface {
	Search(ctx context.Context, query string) ([]Result, error)
}

// Tavily calls the Tavily search API with a server-wide key.
// The key is sent as a bearer token and never appears in an error.
type Tavily struct {
	key      string
	endpoint string
	http     *http.Client
}

// OpenTavily returns nil when key is empty, which leaves web search off.
func OpenTavily(key string) *Tavily {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	return &Tavily{key: key, endpoint: TavilyURL, http: &http.Client{
		Timeout: defaultTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

type tavilyRequest struct {
	Query         string `json:"query"`
	SearchDepth   string `json:"search_depth"`
	MaxResults    int    `json:"max_results"`
	IncludeAnswer bool   `json:"include_answer"`
}

type tavilyResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

// Search returns at most five pages. Results without an http(s) URL are dropped.
func (t *Tavily) Search(ctx context.Context, query string) ([]Result, error) {
	query = clip(strings.Join(strings.Fields(query), " "), maxQueryRunes)
	if query == "" {
		return nil, errors.New("web search: query is required")
	}
	body, err := json.Marshal(tavilyRequest{Query: query, SearchDepth: "basic", MaxResults: maxResults})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("web search: request is not valid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.key)
	res, err := t.http.Do(req)
	if err != nil {
		return nil, errors.New("web search: request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web search: status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return nil, errors.New("web search: read failed")
	}
	var out tavilyResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("web search: response is not json")
	}
	results := make([]Result, 0, len(out.Results))
	for _, row := range out.Results {
		if !webURL(row.URL) {
			continue
		}
		title := clip(strings.TrimSpace(row.Title), maxTitleRunes)
		if title == "" {
			title = row.URL
		}
		results = append(results, Result{Title: title, URL: row.URL, Snippet: clip(strings.TrimSpace(row.Content), maxSnippetRunes)})
		if len(results) == maxResults {
			break
		}
	}
	return results, nil
}

func webURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Fake returns fixed results or an error. Tests and local runs use it.
type Fake struct {
	Results []Result
	Err     error
	Queries []string
}

// Search records the query and returns the fixed reply.
func (f *Fake) Search(_ context.Context, query string) ([]Result, error) {
	f.Queries = append(f.Queries, query)
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Results, nil
}
