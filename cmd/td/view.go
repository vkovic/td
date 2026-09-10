package main

import (
	"time"

	"github.com/vkovic/td/internal/epilogue"
	"github.com/vkovic/td/internal/store"
)

// itemView is an item as the CLI reports it, and the shape of every item in
// --json output. It is the contract the /td plugin reads, so field names stay
// the frontmatter's own wherever there is one.
type itemView struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Tags              []string   `json:"tags"`
	Due               string     `json:"due,omitempty"`
	Created           time.Time  `json:"created"`
	Updated           time.Time  `json:"updated"`
	DoneAt            *time.Time `json:"done_at"`
	Done              bool       `json:"done"`
	Source            string     `json:"source,omitempty"`
	Context           string     `json:"context,omitempty"`
	ClaudeSessionName string     `json:"claude_session_name,omitempty"`
	ClaudeSessionID   string     `json:"claude_session_id,omitempty"`

	// Scope, Area and Path say where the item lives, which frontmatter does not
	// record because the file's location is the record.
	Scope string `json:"scope"`
	Area  string `json:"area"`
	Path  string `json:"path"`

	// Body is included only where the whole item is being reported, so a
	// listing stays small.
	Body string `json:"body,omitempty"`
}

// areaName renders an area for output, naming the live area rather than
// reporting the empty string it is stored as.
func areaName(a store.Area) string {
	if a == store.Active {
		return "active"
	}
	return string(a)
}

// newItemView renders one entry. Bodies are large, so a listing asks for them
// to be left out.
func newItemView(e store.Entry, withBody bool) itemView {
	it := e.Item
	v := itemView{
		ID:                it.ID,
		Title:             it.Title,
		Tags:              it.Tags,
		Created:           it.Created,
		Updated:           it.Updated,
		DoneAt:            it.DoneAt,
		Done:              it.Done(),
		Source:            it.Source,
		Context:           it.Context,
		ClaudeSessionName: it.ClaudeSessionName,
		ClaudeSessionID:   it.ClaudeSessionID,
		Scope:             e.Ref.Scope.String(),
		Area:              areaName(e.Ref.Area),
		Path:              e.Ref.Path,
	}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if it.Due != nil {
		v.Due = it.Due.String()
	}
	if withBody {
		v.Body = it.Body
	}
	return v
}

// newItemViews renders a set of entries.
func newItemViews(entries []store.Entry, withBody bool) []itemView {
	views := make([]itemView, 0, len(entries))
	for _, e := range entries {
		views = append(views, newItemView(e, withBody))
	}
	return views
}

// epilogueView is what the shared tail did, reported alongside every result so
// a caller can tell whether its change reached git.
type epilogueView struct {
	Bumped    []string `json:"bumped"`
	Archived  []string `json:"archived"`
	Committed bool     `json:"committed"`
	Message   string   `json:"message,omitempty"`
	Pushed    bool     `json:"pushed"`
	Warnings  []string `json:"warnings"`
}

// newEpilogueView renders an epilogue result.
func newEpilogueView(res epilogue.Result) epilogueView {
	v := epilogueView{
		Bumped:    res.Bumped,
		Archived:  res.Archived,
		Committed: res.Committed,
		Message:   res.Message,
		Pushed:    res.Pushed,
	}
	if v.Bumped == nil {
		v.Bumped = []string{}
	}
	if v.Archived == nil {
		v.Archived = []string{}
	}
	v.Warnings = make([]string, 0, len(res.Warnings))
	for _, w := range res.Warnings {
		v.Warnings = append(v.Warnings, w.Error())
	}
	return v
}

// mutationResult is what every command that changes the store reports.
type mutationResult struct {
	Action   string       `json:"action"`
	Items    []itemView   `json:"items"`
	Epilogue epilogueView `json:"epilogue"`
}

// now is the timestamp a mutation records: UTC, to the second, matching what
// frontmatter can represent.
func now() time.Time { return store.Now() }
