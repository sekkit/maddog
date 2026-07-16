package repair

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type UpdateTransaction struct {
	dir      string
	mu       sync.Mutex
	prepared bool
	digest   string
	lock     *os.File
}

func NewUpdateTransaction(dir string) *UpdateTransaction { return &UpdateTransaction{dir: dir} }
func (t *UpdateTransaction) stagedPath() string          { return filepath.Join(t.dir, "staged") }
func (t *UpdateTransaction) Prepare(data []byte, digest string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.prepared {
		return fmt.Errorf("update: transaction is already prepared")
	}
	if digest == "" || SHA256(data) != digest {
		return fmt.Errorf("update: verified release digest mismatch")
	}
	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(t.dir, "transaction.lock"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("update: transaction is locked: %w", err)
	}
	if err := os.WriteFile(t.stagedPath(), data, 0o600); err != nil {
		_ = lock.Close()
		_ = os.Remove(filepath.Join(t.dir, "transaction.lock"))
		return err
	}
	t.lock, t.prepared, t.digest = lock, true, digest
	return nil
}

func (t *UpdateTransaction) unlock() {
	if t.lock != nil {
		_ = t.lock.Close()
		t.lock = nil
	}
	_ = os.Remove(filepath.Join(t.dir, "transaction.lock"))
}
func (t *UpdateTransaction) Commit(apply func(path string) error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.prepared {
		return fmt.Errorf("update: transaction is not prepared")
	}
	b, err := os.ReadFile(t.stagedPath())
	if err != nil {
		return err
	}
	if SHA256(b) != t.digest {
		return fmt.Errorf("update: staged release digest changed")
	}
	if err := apply(t.stagedPath()); err != nil {
		return err
	}
	_ = os.Remove(t.stagedPath())
	t.prepared = false
	t.unlock()
	return nil
}

// CommitWithRollback gives platform updaters a single locked boundary around
// replacing an existing release unit. The callback performs the platform-specific
// install; if it fails, the original bytes are restored before the lock is freed.
func (t *UpdateTransaction) CommitWithRollback(currentPath string, apply func(stagedPath, backupPath string) error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.prepared {
		return fmt.Errorf("update: transaction is not prepared")
	}
	staged, err := os.ReadFile(t.stagedPath())
	if err != nil {
		return err
	}
	if SHA256(staged) != t.digest {
		return fmt.Errorf("update: staged release digest changed")
	}
	current, err := os.ReadFile(currentPath)
	if err != nil {
		return err
	}
	backupPath := filepath.Join(t.dir, "current.backup")
	if err := os.WriteFile(backupPath, current, 0o600); err != nil {
		return err
	}
	if err := apply(t.stagedPath(), backupPath); err != nil {
		rollbackErr := os.WriteFile(currentPath, current, 0o600)
		_ = os.Remove(t.stagedPath())
		_ = os.Remove(backupPath)
		t.prepared = false
		t.unlock()
		return errors.Join(err, rollbackErr)
	}
	_ = os.Remove(t.stagedPath())
	_ = os.Remove(backupPath)
	t.prepared = false
	t.unlock()
	return nil
}
func (t *UpdateTransaction) Cancel() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.prepared {
		return nil
	}
	t.prepared = false
	err := os.Remove(t.stagedPath())
	t.unlock()
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
