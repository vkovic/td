package task

import "github.com/vkovic/td/internal/store"

// ResolveAll turns id prefixes into entries, failing before anything is written
// if any one of them does not resolve. An operation that touches several items
// must not half-apply itself because the last id was a typo.
//
// The same item named twice is not an error. Asking for it once is what the
// person meant, and refusing would be pedantry about a duplicate that changes
// nothing.
func (s *Service) ResolveAll(ids []string, scope store.Scope, areas ...store.Area) ([]store.Entry, error) {
	entries := make([]store.Entry, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, prefix := range ids {
		e, err := s.store.Resolve(prefix, scope, areas...)
		if err != nil {
			return nil, err
		}
		if seen[e.Item.ID] {
			continue
		}
		seen[e.Item.ID] = true
		entries = append(entries, e)
	}
	return entries, nil
}
