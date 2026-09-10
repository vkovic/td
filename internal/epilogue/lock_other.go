//go:build !unix

package epilogue

import (
	"errors"
	"os"
)

// lock is a placeholder on platforms without flock. td targets tmux on Unix, so
// this path exists to keep the package buildable rather than to be used.
type lock struct {
	f *os.File
}

// acquireLock refuses to run rather than pretending the store is protected.
func acquireLock(path string) (*lock, error) {
	return nil, errors.New("td needs file locking, which this platform does not provide")
}

func (l *lock) release() error { return nil }
