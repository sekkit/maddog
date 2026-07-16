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

var ErrSessionLeaseHeld = errors.New("session lease held by another runtime")
var sessionLeaseOwners sync.Map
var sessionLeaseSeq atomic.Uint64

type SessionLeaseInfo struct {
	SessionPath string    `json:"session_path"`
	WriterID    string    `json:"writer_id"`
	PID         int       `json:"pid"`
	Hostname    string    `json:"hostname,omitempty"`
	AcquiredAt  time.Time `json:"acquired_at"`
}
type SessionLeaseError struct {
	Path string
	Info *SessionLeaseInfo
}

func (e *SessionLeaseError) Error() string {
	if e == nil {
		return ErrSessionLeaseHeld.Error()
	}
	if e.Info != nil && e.Info.WriterID != "" {
		return fmt.Sprintf("%s: %s is held by %s", ErrSessionLeaseHeld, e.Path, e.Info.WriterID)
	}
	return fmt.Sprintf("%s: %s", ErrSessionLeaseHeld, e.Path)
}
func (e *SessionLeaseError) Unwrap() error { return ErrSessionLeaseHeld }

type SessionLease struct {
	path   string
	owner  uint64
	unlock func()
	once   sync.Once
}

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
func (l *SessionLease) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}
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
func SessionWriterID() string {
	return fmt.Sprintf("%s-%d", strings.TrimSpace(func() string { h, _ := os.Hostname(); return h }()), os.Getpid())
}
func newSessionLeaseInfo(path string) SessionLeaseInfo {
	h, _ := os.Hostname()
	return SessionLeaseInfo{SessionPath: path, WriterID: SessionWriterID(), PID: os.Getpid(), Hostname: h, AcquiredAt: time.Now().UTC()}
}
func LoadSessionLeaseInfo(path string) (*SessionLeaseInfo, error) {
	b, e := os.ReadFile(store.SessionLeaseInfo(path))
	if e != nil {
		return nil, e
	}
	var i SessionLeaseInfo
	e = json.Unmarshal(b, &i)
	return &i, e
}
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
