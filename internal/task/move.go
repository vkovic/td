package task

import "github.com/vkovic/td/internal/store"

// MoveRequest moves items between areas.
type MoveRequest struct {
	// Scope is the list the ids are looked up in.
	Scope store.Scope

	// IDs are id prefixes.
	IDs []string
}

// Remove moves items to the trash.
//
// deleted/ is gitignored and nothing purges it, so this is the one operation
// whose result is not in the history: recovering an item means going to the
// directory rather than to git.
//
// The action it reports is "remove" and not "rm". That string is the /td
// plugin's contract and cmd/td branches on the exact word to decide whether
// item bodies appear in --json, so the command's name and the action it reports
// are deliberately not the same thing.
func (s *Service) Remove(req MoveRequest) (Result, error) {
	return s.moveTo("remove", req, store.Deleted, store.Active, store.Archived)
}

// Restore returns items to the live list, from the trash or from the archive.
func (s *Service) Restore(req MoveRequest) (Result, error) {
	return s.moveTo("restore", req, store.Active, store.Deleted, store.Archived)
}

// moveTo resolves the request's ids across from, files each item under to, and
// describes what it did. Moving is a rename, so the entry's ref changes and the
// caller is handed the new one.
func (s *Service) moveTo(action string, req MoveRequest, to store.Area, from ...store.Area) (Result, error) {
	entries, err := s.ResolveAll(req.IDs, req.Scope, from...)
	if err != nil {
		return Result{}, err
	}

	stamp := s.now()
	for i, e := range entries {
		e.Item.Updated = stamp
		ref, err := s.store.Move(e.Ref, e.Item, e.Ref.Scope, to)
		if err != nil {
			return Result{}, err
		}
		entries[i].Ref = ref
	}
	return result(action, entries), nil
}
