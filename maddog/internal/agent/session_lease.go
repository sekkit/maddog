package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"maddog/internal/fileutil"
	store "maddog/internal/store"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrSessionLeaseHeld reports that another runtime owns the session writer lease.
var ErrSessionLeaseHeld = errors.New("session lease held by another runtime")
var sessionLeaseOwners sync.Map
var sessionLeaseSeq atomic.Uint64

// SessionLeaseInfo identifies the runtime that owns a session writer lease.
type SessionLeaseInfo struct {
	SessionPath string    `json:"session_path"`
	WriterID    string    `json:"writer_id"`
	PID         int       `json:"pid"`
	Hostname    string    `json:"hostname,omitempty"`
	AcquiredAt  time.Time `json:"acquired_at"`
}

// SessionLeaseError describes a session lease conflict and its current owner.
type SessionLeaseError struct {
	Path string
	Info *SessionLeaseInfo
}

// Error returns a human-readable description of the session lease conflict.
func (e *SessionLeaseError) Error() string {
	if e == nil {
		return ErrSessionLeaseHeld.Error()
	}
	if e.Info != nil && e.Info.WriterID != "" {
		return fmt.Sprintf("%s: %s is held by %s", ErrSessionLeaseHeld, e.Path, e.Info.WriterID)
	}
	return fmt.Sprintf("%s: %s", ErrSessionLeaseHeld, e.Path)
}

// Unwrap makes SessionLeaseError comparable with ErrSessionLeaseHeld.
func (e *SessionLeaseError) Unwrap() error { return ErrSessionLeaseHeld }

// SessionLease grants this process exclusive writer access to one session.
type SessionLease struct {
	path   string
	owner  uint64
	unlock func()
	once   sync.Once
}

// TryAcquireSessionLease acquires exclusive writer access to path if it is available.
func TryAcquireSessionLease(path string) (*SessionLease, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return nil, fmt.Errorf("empty session path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	id := sessionLeaseSeq.Add(1)
	if _, loaded := sessionLeaseOwners.LoadOrStore(path, id); loaded {
		info, _ := LoadSessionLeaseInfo(path)
		return nil, &SessionLeaseError{Path: path, Info: info}
	}
	unlock, err := tryLockSessionLeaseFile(store.SessionLeaseLock(path))
	if err != nil {
		sessionLeaseOwners.CompareAndDelete(path, id)
		if errors.Is(err, ErrSessionLeaseHeld) {
			info, _ := LoadSessionLeaseInfo(path)
			return nil, &SessionLeaseError{Path: path, Info: info}
		}
		return nil, err
	}
	l := &SessionLease{path: path, owner: id, unlock: unlock}
	if err := SaveSessionLeaseInfo(path, newSessionLeaseInfo(path)); err != nil {
		l.Release()
		return nil, err
	}
	return l, nil
}

// TryReclaimCurrentProcessSessionLease reacquires a session lease already owned by this process.
func TryReclaimCurrentProcessSessionLease(path string) (*SessionLease, error) {
	path = filepath.Clean(path)
	unlock, err := tryLockSessionLeaseFile(store.SessionLeaseLock(path))
	if err != nil {
		return nil, err
	}
	id := sessionLeaseSeq.Add(1)
	sessionLeaseOwners.Store(path, id)
	l := &SessionLease{path: path, owner: id, unlock: unlock}
	if err := SaveSessionLeaseInfo(path, newSessionLeaseInfo(path)); err != nil {
		l.Release()
		return nil, err
	}
	return l, nil
}

// Path returns the session path protected by the lease.
func (l *SessionLease) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Release relinquishes the lease and removes its owner metadata.
func (l *SessionLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		_ = os.Remove(store.SessionLeaseInfo(l.path))
		if l.unlock != nil {
			l.unlock()
		}
		sessionLeaseOwners.CompareAndDelete(l.path, l.owner)
	})
}

// SessionLeaseHeldByOtherRuntime reports whether another process owns path's writer lease.
func SessionLeaseHeldByOtherRuntime(path string) bool {
	path = filepath.Clean(path)
	if _, ok := sessionLeaseOwners.Load(path); ok {
		return false
	}
	info, err := LoadSessionLeaseInfo(path)
	if os.IsNotExist(err) {
		return false
	}
	unlock, lockErr := tryLockSessionLeaseFile(store.SessionLeaseLock(path))
	if lockErr == nil {
		_ = os.Remove(store.SessionLeaseInfo(path))
		unlock()
		return false
	}
	return info != nil
}

func sessionLeaseOwned(path string) bool {
	_, ok := sessionLeaseOwners.Load(filepath.Clean(path))
	return ok
}

// SessionWriterID returns the stable writer identity for the current process.
func SessionWriterID() string {
	return fmt.Sprintf("%s-%d", strings.TrimSpace(func() string { h, _ := os.Hostname(); return h }()), os.Getpid())
}
func newSessionLeaseInfo(path string) SessionLeaseInfo {
	h, _ := os.Hostname()
	return SessionLeaseInfo{SessionPath: path, WriterID: SessionWriterID(), PID: os.Getpid(), Hostname: h, AcquiredAt: time.Now().UTC()}
}

// LoadSessionLeaseInfo loads the persisted owner metadata for path.
func LoadSessionLeaseInfo(path string) (*SessionLeaseInfo, error) {
	b, e := os.ReadFile(store.SessionLeaseInfo(path))
	if e != nil {
		return nil, e
	}
	var i SessionLeaseInfo
	e = json.Unmarshal(b, &i)
	return &i, e
}

// SaveSessionLeaseInfo atomically persists private owner metadata for path.
func SaveSessionLeaseInfo(path string, info SessionLeaseInfo) error {
	b, e := json.MarshalIndent(info, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	if err := fileutil.EnsurePrivateDir(filepath.Dir(store.SessionLeaseInfo(path))); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(store.SessionLeaseInfo(path), b, 0o600)
}
