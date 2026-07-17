package repair

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// UpdateTransaction serializes a verified staged update through commit or cancellation.
type UpdateTransaction struct {
	dir      string
	mu       sync.Mutex
	prepared bool
	digest   string
	lock     *os.File
}

type fileSnapshot struct {
	path string
	data []byte
	mode os.FileMode
}

// NewUpdateTransaction creates an update transaction using dir for private staging state.
func NewUpdateTransaction(dir string) *UpdateTransaction { return &UpdateTransaction{dir: dir} }
func (t *UpdateTransaction) stagedPath() string          { return filepath.Join(t.dir, "staged") }

// Prepare verifies and stages data while exclusively locking the transaction directory.
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

// Commit applies the verified staged file and releases the transaction lock on success.
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
	currentInfo, err := os.Stat(currentPath)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(currentPath)
	if err != nil {
		return err
	}
	backupPath := filepath.Join(t.dir, "current.backup")
	if err := os.WriteFile(backupPath, current, currentInfo.Mode().Perm()); err != nil {
		return err
	}
	if err := apply(t.stagedPath(), backupPath); err != nil {
		rollbackErr := restoreAtomic(currentPath, current, currentInfo.Mode())
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

// CommitReleaseUnitWithRollback applies one verified release archive to all
// listed existing files and restores every file if the release-unit update fails.
func (t *UpdateTransaction) CommitReleaseUnitWithRollback(paths []string, apply func(stagedPath string) error) error {
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
	snapshots := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshots = append(snapshots, fileSnapshot{path: path, data: data, mode: info.Mode()})
	}
	if err := apply(t.stagedPath()); err != nil {
		rollbackErrors := []error{err}
		for _, snapshot := range snapshots {
			rollbackErrors = append(rollbackErrors, restoreAtomic(snapshot.path, snapshot.data, snapshot.mode))
		}
		_ = os.Remove(t.stagedPath())
		t.prepared = false
		t.unlock()
		return errors.Join(rollbackErrors...)
	}
	_ = os.Remove(t.stagedPath())
	t.prepared = false
	t.unlock()
	return nil
}

func restoreAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".maddog-rollback-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(mode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return replacePath(tmpPath, path)
}

// ReplaceFileAtomic replaces path with synced data while preserving the supplied mode.
func ReplaceFileAtomic(path string, data []byte, mode os.FileMode) error {
	return restoreAtomic(path, data, mode)
}

// Cancel removes staged data and releases the transaction lock.
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
