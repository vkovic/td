// Package task owns the operations td performs on items: creating one,
// completing it, removing it, editing it. Both surfaces call in here — the
// cobra CLI in cmd/td and the Bubble Tea interface in internal/tui — so an
// operation has one implementation and the two cannot drift apart.
//
// They had drifted. Adding, completing and removing were each written twice,
// once against the store in each surface, and the two commitMessage helpers
// carried comments telling the reader they had to agree with one another. That
// agreement was kept by discipline, which is the kind that lapses quietly.
//
// A Service mutates the store and reports what it did. It deliberately does not
// run the epilogue. The CLI runs that synchronously; the TUI runs it as a
// tea.Cmd off its event loop, because the epilogue takes a blocking flock and
// waiting on it in the UI goroutine would freeze the pane. What the epilogue
// does is shared policy, when it runs is each surface's own business, and this
// is the seam that was already there.
package task

import (
	"time"

	"github.com/vkovic/td/internal/store"
)

// Service performs item operations against one open store.
type Service struct {
	store *store.Store

	// now stamps created, updated and done_at.
	//
	// store.Now and never time.Now: Save pins a file's mtime to Item.Updated
	// and the frontmatter carries updated to whole seconds, so a clock with
	// nanoseconds in it leaves the mtime later than the timestamp it reads back
	// as — which is exactly what td reads as an edit made outside td, and the
	// next bump then restamps everything.
	now func() time.Time

	// newID mints item ids. store.NewID by default, and deliberately not a
	// generator of this Service's own: a generator holds the last id it handed
	// out so that ids minted inside one millisecond still differ, and two
	// generators do not share that counter. Running the pane builds two
	// Services in one process — cmd/td makes one in setup and internal/tui
	// makes another — so a Service minting from its own counter would be two
	// items created in the same millisecond away from issuing the same id
	// twice.
	//
	// Separate from now on purpose: an id encodes a millisecond, so it reads a
	// clock that has not been truncated to the second. See store.NewIDGen,
	// which says why at length.
	newID func() string
}

// Option adjusts a Service as it is built.
type Option func(*Service)

// WithIDGen replaces the id source, so a test can pin the clock behind a
// generator of its own and read back an exact sequence.
func WithIDGen(g *store.IDGen) Option {
	return func(s *Service) { s.newID = g.Next }
}

// New returns a Service over an already open store. A nil clock means store.Now.
func New(st *store.Store, now func() time.Time, opts ...Option) *Service {
	if now == nil {
		now = store.Now
	}
	s := &Service{store: st, now: now, newID: store.NewID}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Result is what an operation did.
type Result struct {
	// Action names the operation the way the CLI reports it in --json. These
	// are the /td plugin's contract rather than internal labels: removing an
	// item reports "remove" and not "rm", and cmd/td branches on that exact
	// word to decide whether item bodies appear in the JSON.
	Action string

	// Entries are the items the operation touched, in the order it took them.
	Entries []store.Entry

	// Message describes the change for git. The caller hands it to the
	// epilogue if and when it chooses to run one.
	Message string
}

// result pairs what an operation did with the message describing it, so no
// operation has to remember to build the message the same way as the others.
func result(action string, entries []store.Entry) Result {
	return Result{Action: action, Entries: entries, Message: CommitMessage(action, entries)}
}
