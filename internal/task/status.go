package task

import "github.com/vkovic/td/internal/store"

// StatusRequest marks items done, or reopens them.
type StatusRequest struct {
	// Scope is the list the ids are looked up in. The CLI passes the scope the
	// invocation resolved to; the TUI passes the scope of the row it is on,
	// which is what keeps the merged view — where rows span scopes — honest.
	Scope store.Scope

	// IDs are id prefixes. The CLI passes what was typed, which may be short;
	// the TUI passes the selected item's full id.
	IDs []string

	// Done says which way. The CLI's two commands each pass a fixed value; the
	// TUI passes the opposite of what the selected item already is.
	Done bool
}

// SetDone stamps done_at on each item, or clears it. Stamping done_at is what
// makes an item done: it stays in the list either way until the archive sweep
// moves it out, done_ttl_days after it was completed.
//
// Reopening looks in the archive as well, so an item already swept out can be
// brought back by undoing it — and is moved back into the list when it is.
func (s *Service) SetDone(req StatusRequest) (Result, error) {
	areas := []store.Area{store.Active}
	action := "done"
	if !req.Done {
		areas = append(areas, store.Archived)
		action = "undo"
	}

	entries, err := s.ResolveAll(req.IDs, req.Scope, areas...)
	if err != nil {
		return Result{}, err
	}

	// One instant across the batch, so done_at and updated agree on each item
	// and two items completed together read as completed together.
	stamp := s.now()
	for i, e := range entries {
		it := e.Item
		if req.Done {
			at := stamp
			it.DoneAt = &at
		} else {
			it.DoneAt = nil
		}
		it.Updated = stamp

		// An item reopened out of the archive belongs back in the list.
		if !req.Done && e.Ref.Area == store.Archived {
			ref, err := s.store.Move(e.Ref, it, e.Ref.Scope, store.Active)
			if err != nil {
				return Result{}, err
			}
			entries[i].Ref = ref
			continue
		}
		if _, err := s.store.Save(e.Ref.Scope, e.Ref.Area, it); err != nil {
			return Result{}, err
		}
	}
	return result(action, entries), nil
}
