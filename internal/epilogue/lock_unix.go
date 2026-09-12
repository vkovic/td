//go:build unix

package epilogue

import (
	"fmt"
	"os"
	"syscall"
)

// lock is an exclusive advisory lock held on a file for the length of a run.
type lock struct {
	f *os.File
}

// acquireLock takes an exclusive lock on path, creating the file if needed and
// blocking until any other holder releases it. Blocking is the point: a second
// td process must wait its turn rather than run the same rewrite and rename
// concurrently, or race the first one on .git/index.
func acquireLock(path string) (*lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening the td lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return &lock{f: f}, nil
}

// release drops the lock. The file is left in place; the lock lives on the open
// descriptor, not on the file existing.
func (l *lock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	closeErr := l.f.Close()
	l.f = nil
	if err != nil {
		return fmt.Errorf("unlocking: %w", err)
	}
	return closeErr
}
