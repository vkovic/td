package task

import (
	"errors"
	"strings"

	"github.com/vkovic/td/internal/store"
)

// ErrEmptyTitle is returned for a request carrying no title. A caller that took
// the title from a person maps it onto whatever it does with bad input: the CLI
// reports a usage error, the TUI treats an empty prompt as a cancelled add.
var ErrEmptyTitle = errors.New("an item needs a title")

// AddRequest is one item to create.
type AddRequest struct {
	// Scope is the list the item is filed in: for the CLI the scope the
	// invocation resolved to, for the TUI the list on screen.
	Scope store.Scope

	Title string
	Tags  []string
	Due   *store.Date
	Body  string

	// Source, SessionName and SessionID record where the item came from, so an
	// item raised mid-session can be traced back to what the session was doing.
	// The CLI fills them from its provenance flags; the TUI names itself.
	//
	// Only a creation writes them. Nothing that edits an item later touches
	// these, because they say who raised the item and not who last typed.
	Source      string
	SessionName string
	SessionID   string
}

// Add creates one item and files it as active in the request's scope. It does
// not commit; the caller runs the epilogue when it suits them.
func (s *Service) Add(req AddRequest) (Result, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return Result{}, ErrEmptyTitle
	}

	// One instant for both stamps. An item nobody has edited reads as created
	// and updated at the same moment, which is what the hand-edit check
	// compares a file's mtime against.
	created := s.now()
	it := &store.Item{
		ID:                s.ids.Next(),
		Title:             title,
		Tags:              req.Tags,
		Due:               req.Due,
		Created:           created,
		Updated:           created,
		Source:            req.Source,
		Context:           store.WorkingContext(),
		ClaudeSessionName: req.SessionName,
		ClaudeSessionID:   req.SessionID,
		Body:              req.Body,
	}

	ref, err := s.store.Save(req.Scope, store.Active, it)
	if err != nil {
		return Result{}, err
	}
	return result("add", []store.Entry{{Item: it, Ref: ref}}), nil
}
