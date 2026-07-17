// Package processlock provides a crash-safe, cross-process file lease.
package processlock

import (
	"os"
	"path/filepath"
	"sync"
)

// Lock is held until Release is called or the process exits.
type Lock struct {
	mu   sync.Mutex
	file *os.File
}

// Try acquires an exclusive lock without waiting. A false acquired result means
// another live process owns the lock; stale files never prevent acquisition.
func Try(path string) (lock *Lock, acquired bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	acquired, err = tryLockFile(f)
	if err != nil || !acquired {
		_ = f.Close()
		return nil, acquired, err
	}
	return &Lock{file: f}, true, nil
}

// Release relinquishes the lease. It is safe to call more than once.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := unlockFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
