package oauth

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// acquireLock takes a blocking exclusive lock on path, creating the parent
// directory if needed. The caller must Unlock the returned lock.
func acquireLock(path string) (*flock.Flock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	fl := flock.New(path)
	if err := fl.Lock(); err != nil {
		return nil, fmt.Errorf("acquire refresh lock: %w", err)
	}
	return fl, nil
}
