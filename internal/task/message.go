package task

import (
	"fmt"

	"github.com/vkovic/td/internal/store"
)

// CommitMessage describes a change for git: the action, and the item it acted
// on when there was exactly one.
//
// Exported because the TUI needs the rule at a different moment than a Result
// carries it. An item added there is saved first and opened in the editor, and
// nothing is committed until the editor exits, so the TUI composes the message
// then — from the entry it is holding rather than from the Result it got back.
// One implementation either way, which is the point: the store's history reads
// the same whichever surface wrote it.
func CommitMessage(action string, entries []store.Entry) string {
	switch len(entries) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("td: %s %s %s", action, entries[0].Item.ID, entries[0].Item.Title)
	default:
		return fmt.Sprintf("td: %s %d items", action, len(entries))
	}
}
