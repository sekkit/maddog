package control

import (
	"maddog/internal/agent"
	"strings"
	"sync"
)

// SessionLeaseKeeper follows the controller's writable transcript path. It
// acquires the new path before releasing the old one, preventing a replacement
// window in which neither runtime owns the canonical transcript.
type SessionLeaseKeeper struct {
	mu    sync.Mutex
	lease *agent.SessionLease
}

func NewSessionLeaseKeeper() *SessionLeaseKeeper { return &SessionLeaseKeeper{} }
func (k *SessionLeaseKeeper) Rebind(path string) error {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	path = strings.TrimSpace(path)
	if path == "" {
		k.releaseLocked()
		return nil
	}
	if k.lease != nil && k.lease.Path() == path {
		return nil
	}
	l, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		return err
	}
	k.releaseLocked()
	k.lease = l
	return nil
}
func (k *SessionLeaseKeeper) Release() {
	if k == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.releaseLocked()
}
func (k *SessionLeaseKeeper) HeldPath() string {
	if k == nil {
		return ""
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.lease == nil {
		return ""
	}
	return k.lease.Path()
}
func (k *SessionLeaseKeeper) releaseLocked() {
	if k.lease != nil {
		k.lease.Release()
		k.lease = nil
	}
}
