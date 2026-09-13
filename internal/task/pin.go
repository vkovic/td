package task

import "github.com/vkovic/td/internal/store"

// PinRequest pins items, or unpins them.
type PinRequest struct {
	// Scope is the list the ids are looked up in: the scope the CLI resolved
	// to, or the scope of the row the TUI's cursor is on.
	Scope store.Scope

	// IDs are id prefixes.
	IDs []string

	// Pinned says which way. td pin and td unpin each pass a fixed value; the
	// TUI passes the opposite of what the selected item already is.
	Pinned bool
}

// SetPinned pins each item, or unpins it. A pinned item heads its section in
// every listing (see store.SortEntries), and nothing else about it changes.
//
// It does not stamp updated. A pin says where an item sits, not that anyone
// worked on it, and updated is both the open section's sort key and the age the
// pane prints: stamping it would claim the item was just touched. The write
// still goes through Save, which pins the file's mtime to the unchanged updated,
// so the next bump does not take td's own write for a hand edit.
//
// An item already the way the request asks is not rewritten, which is what makes
// td pin idempotent without leaving a commit behind. It is still reported, so a
// caller sees the state it asked for.
//
// The one stamp is for a file edited by hand and not yet bumped. Save would pull
// its mtime back to updated and erase the only evidence of that edit, so the
// edit is recorded here instead, with the stamp the bump was about to write.
func (s *Service) SetPinned(req PinRequest) (Result, error) {
	action := "pin"
	if !req.Pinned {
		action = "unpin"
	}

	entries, err := s.ResolveAll(req.IDs, req.Scope, store.Active)
	if err != nil {
		return Result{}, err
	}

	for _, e := range entries {
		it := e.Item
		if it.Pinned == req.Pinned {
			continue
		}
		edited, err := store.HandEdited(e)
		if err != nil {
			return Result{}, err
		}
		if edited {
			it.Updated = s.now()
		}
		it.Pinned = req.Pinned
		if _, err := s.store.Save(e.Ref.Scope, e.Ref.Area, it); err != nil {
			return Result{}, err
		}
	}
	return result(action, entries), nil
}
