package task

import (
	"errors"
	"strings"

	"github.com/vkovic/td/internal/store"
)

// ErrNothingToChange is returned for a Change that sets no field. A caller
// taking the change from a person says which of its own knobs were available.
var ErrNothingToChange = errors.New("the change sets nothing")

// Change is what an edit sets. Every field is optional, and a nil one is left
// alone — td edit's oldest rule, that a flag you did not type never blanks a
// field.
//
// It is a struct rather than the callback it replaced. cmd/td used to hand the
// operation cobra's "was this flag typed" function and ask it six times, which
// made "was it typed" and "should it change" the same question in a place that
// had no business knowing about flags at all.
type Change struct {
	Title  *string
	Body   *string
	Append *string
	Tags   *[]string

	// Due carries three states where the rest carry two: nil leaves the due
	// date alone, ClearDue removes it, SetDue replaces it. td edit --due ""
	// clears the field, which is a different thing from not passing --due.
	Due *DueChange
}

// DueChange is a due date to set, or the absence of one to clear.
type DueChange struct{ date *store.Date }

// SetDue changes an item's due date.
func SetDue(d store.Date) *DueChange { return &DueChange{date: &d} }

// ClearDue removes an item's due date. It is a separate constructor so that
// clearing has to be asked for, and cannot be the accident of a zero value.
func ClearDue() *DueChange { return &DueChange{} }

// Empty reports whether the change would do nothing.
func (c Change) Empty() bool {
	return c.Title == nil && c.Body == nil && c.Append == nil && c.Tags == nil && c.Due == nil
}

// Edit applies one change to every entry and saves each.
//
// It takes resolved entries where the other operations take ids, because the
// caller has to resolve first. A change carrying a body read from a file must
// open that file to build the Change, and resolving after that would report an
// unreadable file for a command whose real fault was an id that does not exist
// — a different failure to the person, and a different exit code.
func (s *Service) Edit(entries []store.Entry, ch Change) (Result, error) {
	if ch.Empty() {
		return Result{}, ErrNothingToChange
	}

	stamp := s.now()
	for _, e := range entries {
		it := e.Item
		if ch.Title != nil {
			// Not TrimSpace, unlike Add: a title of spaces is refused on the
			// way in and tolerated on the way through, which is how td has
			// always behaved and is not this change's business to settle.
			if *ch.Title == "" {
				return Result{}, ErrEmptyTitle
			}
			it.Title = *ch.Title
		}
		if ch.Body != nil {
			it.Body = *ch.Body
		}
		if ch.Append != nil {
			it.Body = appendParagraph(it.Body, *ch.Append)
		}
		if ch.Tags != nil {
			// Replaces rather than adds. --tag is how the whole set is stated.
			it.Tags = *ch.Tags
		}
		if ch.Due != nil {
			it.Due = ch.Due.date
		}
		it.Updated = stamp

		if _, err := s.store.Save(e.Ref.Scope, e.Ref.Area, it); err != nil {
			return Result{}, err
		}
	}
	return result("edit", entries), nil
}

// NormalizeBody trims a body to the shape an item file stores: no leading or
// trailing blank lines, and one closing newline when there is anything at all.
// Exported because a surface reading a body from a file or a pipe normalizes it
// before it ever becomes a Change.
func NormalizeBody(s string) string {
	s = strings.Trim(s, "\n")
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s + "\n"
}

// appendParagraph adds text to a body, separated by a blank line so the result
// is still readable markdown.
func appendParagraph(body, text string) string {
	text = NormalizeBody(text)
	if text == "" {
		return body
	}
	if body == "" {
		return text
	}
	return body + "\n" + text
}
