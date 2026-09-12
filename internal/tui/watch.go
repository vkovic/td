package tui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/vkovic/td/internal/store"
)

// defaultDebounce is how long the watcher waits for a burst to finish before
// reporting it. One td command writes a file, renames it, writes another and
// then commits, which is a dozen events for one change; a person watching the
// pane wants one refresh out of that, and wants it inside a second.
const defaultDebounce = 150 * time.Millisecond

// watcher reports that something under the store changed, without saying what.
// The model re-reads on a pulse; it never acts on the event itself.
//
// It watches the item directories only — the store root and each project — and
// never recursively. Watching .git/ would turn every commit td makes into an
// event storm, and deleted/ and archived/ are out of the list, so a change
// there is not a change to anything on screen.
type watcher struct {
	fsw      *fsnotify.Watcher
	pulses   chan struct{}
	done     chan struct{}
	root     string
	debounce time.Duration
}

// newWatcher starts watching a store's item directories.
func newWatcher(s *store.Store, debounce time.Duration) (*watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if debounce <= 0 {
		debounce = defaultDebounce
	}
	w := &watcher{
		fsw:      fsw,
		pulses:   make(chan struct{}, 1),
		done:     make(chan struct{}),
		root:     s.Root(),
		debounce: debounce,
	}

	// The root first: it is where a new project directory appears, and where
	// the global list's own items live.
	if err := w.add(w.root); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	scopes, err := s.Scopes()
	if err != nil {
		_ = fsw.Close()
		return nil, err
	}
	for _, scope := range scopes {
		if scope.IsGlobal() {
			continue
		}
		// A project directory that vanished between the listing and here is
		// not an error: the next event on the root will bring it back.
		_ = w.add(s.Dir(scope, store.Active))
	}

	go w.run()
	return w, nil
}

// add starts watching one directory.
func (w *watcher) add(dir string) error { return w.fsw.Add(dir) }

// Pulses delivers one value per settled burst of changes.
func (w *watcher) Pulses() <-chan struct{} { return w.pulses }

// Close stops watching.
func (w *watcher) Close() error {
	select {
	case <-w.done:
	default:
		close(w.done)
	}
	return w.fsw.Close()
}

// run collects events and reports a burst once it has settled.
func (w *watcher) run() {
	var timer <-chan time.Time
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			// A new project directory has to be watched as well as reported,
			// or the items created inside it would never be seen.
			if ev.Has(fsnotify.Create) && w.isProjectDir(ev.Name) {
				_ = w.add(ev.Name)
			}
			if !w.interesting(ev.Name) {
				continue
			}
			timer = time.After(w.debounce)
		case <-w.fsw.Errors:
			// A watch error costs this event, not the watcher. The next change
			// under the store reports itself normally.
			continue
		case <-timer:
			timer = nil
			select {
			case w.pulses <- struct{}{}:
			default:
				// A pulse is already waiting to be read. Two would mean two
				// re-reads of the same state.
			}
		}
	}
}

// interesting reports whether a path is one whose change the list would show.
func (w *watcher) interesting(path string) bool {
	name := filepath.Base(path)
	if name == store.LockName || strings.HasPrefix(name, ".td-") {
		return false
	}
	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		return false
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		switch part {
		case ".git", string(store.Deleted), string(store.Archived):
			return false
		}
	}
	// Everything that survives the exclusions counts, including a directory
	// appearing or vanishing: that changes which lists exist, which the merged
	// view shows. Being generous here costs a re-read, which is cheap and
	// idempotent; being strict would cost a change that never appears.
	return true
}

// isProjectDir reports whether a created path is a new project directory
// directly under the root, and so needs a watch of its own.
func (w *watcher) isProjectDir(path string) bool {
	if filepath.Dir(path) != w.root {
		return false
	}
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") || name == string(store.Archived) || name == string(store.Deleted) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
