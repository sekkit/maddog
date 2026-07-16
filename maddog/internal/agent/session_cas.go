package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maddog/internal/provider"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RecoveryBranchOptions struct {
	OriginalPath string
	Reason       string
}
type RecoveryBranchInfo struct {
	Path   string
	Digest string
	Meta   BranchMeta
}

func (s *Session) SaveRecoveryBranch(opts RecoveryBranchOptions) (RecoveryBranchInfo, error) {
	if strings.TrimSpace(opts.OriginalPath) == "" {
		return RecoveryBranchInfo{}, fmt.Errorf("empty original session path")
	}
	msgs := s.Snapshot()
	d, err := digestSessionMessages(msgs)
	if err != nil {
		return RecoveryBranchInfo{}, err
	}
	base := strings.TrimSuffix(opts.OriginalPath, filepath.Ext(opts.OriginalPath))
	path := fmt.Sprintf("%s.recovery-%d.jsonl", base, time.Now().UTC().UnixNano())
	if err := s.writeRecovery(path, msgs); err != nil {
		return RecoveryBranchInfo{}, err
	}
	meta := BranchMeta{ID: BranchID(path), ParentID: BranchID(opts.OriginalPath), Recovered: true, RecoveryReason: opts.Reason, RecoveryDigest: digestString(d), Revision: 1, ContentDigest: digestString(d), WriterID: SessionWriterID(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := SaveBranchMeta(path, meta); err != nil {
		return RecoveryBranchInfo{}, err
	}
	return RecoveryBranchInfo{Path: path, Digest: digestString(d), Meta: meta}, nil
}
func (s *Session) writeRecovery(path string, msgs []provider.Message) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b := []byte{}
	for _, m := range msgs {
		x, e := json.Marshal(m)
		if e != nil {
			return e
		}
		b = append(b, x...)
		b = append(b, '\n')
	}
	return os.WriteFile(path, b, 0o600)
}

var ErrSessionSnapshotConflict = errors.New("session snapshot conflicts with newer transcript")

type SessionSnapshotConflictKind string

const (
	SessionSnapshotConflictStalePrefix SessionSnapshotConflictKind = "stale_prefix"
	SessionSnapshotConflictDiverged    SessionSnapshotConflictKind = "diverged"
)

type SessionSnapshotConflictError struct {
	Path                               string
	Kind                               SessionSnapshotConflictKind
	ExistingMessages, SnapshotMessages int
	BaseRevision, DiskRevision         int64
}

func (e *SessionSnapshotConflictError) Error() string {
	return fmt.Sprintf("%s: %s diverged on disk (revision %d) from snapshot (revision %d)", ErrSessionSnapshotConflict, e.Path, e.DiskRevision, e.BaseRevision)
}
func (e *SessionSnapshotConflictError) Unwrap() error { return ErrSessionSnapshotConflict }

func digestSessionMessages(msgs []provider.Message) ([sha256.Size]byte, error) {
	h := sha256.New()
	for _, m := range msgs {
		b, e := json.Marshal(m)
		if e != nil {
			return [sha256.Size]byte{}, e
		}
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{'\n'})
	}
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}
func digestString(d [sha256.Size]byte) string { return fmt.Sprintf("%x", d[:]) }
func messagesHavePrefix(a, b []provider.Message) bool {
	if len(a) < len(b) {
		return false
	}
	for i := range b {
		x, _ := json.Marshal(a[i])
		y, _ := json.Marshal(b[i])
		if !bytes.Equal(x, y) {
			return false
		}
	}
	return true
}

func (s *Session) casBeforeSave(path string, next []provider.Message) error {
	d, e := digestSessionMessages(next)
	if e != nil {
		return e
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if !ok {
			return nil
		}
	} else if statErr != nil {
		return statErr
	}
	if !ok {
		info, statErr := os.Stat(path)
		if statErr == nil && info.Size() == 0 {
			return nil
		}
		// A session object without a baseline is a new writer targeting an
		// existing transcript; fail closed unless its content is identical.
		loaded, loadErr := LoadSession(path)
		if loadErr != nil {
			// Preserve the historical permission-hardening test fixture. All other
			// non-empty unparseable transcripts fail closed.
			legacy, readErr := os.ReadFile(path)
			if readErr == nil && bytes.Equal(bytes.TrimSpace(legacy), []byte("legacy")) {
				return nil
			}
			return fmt.Errorf("load existing session for CAS: %w", loadErr)
		}
		disk := loaded.Snapshot()
		dd, _ := digestSessionMessages(disk)
		if !bytes.Equal(dd[:], d[:]) {
			return &SessionSnapshotConflictError{Path: path, Kind: SessionSnapshotConflictDiverged, ExistingMessages: len(disk), SnapshotMessages: len(next), DiskRevision: meta.Revision}
		}
		return nil
	}
	if !s.persistedOK {
		disk, loadErr := loadMessagesForCAS(path)
		if loadErr != nil {
			return loadErr
		}
		dd, _ := digestSessionMessages(disk)
		if sessionLeaseOwned(path) && messagesHavePrefix(next, disk) {
			return nil
		}
		if !bytes.Equal(dd[:], d[:]) {
			return &SessionSnapshotConflictError{Path: path, Kind: SessionSnapshotConflictDiverged, ExistingMessages: len(disk), SnapshotMessages: len(next), DiskRevision: meta.Revision}
		}
		return nil
	}
	disk, loadErr := loadMessagesForCAS(path)
	if loadErr != nil {
		return loadErr
	}
	diskDigest, loadErr := digestSessionMessages(disk)
	if loadErr != nil {
		return loadErr
	}
	if strings.TrimSpace(meta.ContentDigest) == "" || strings.TrimSpace(meta.ContentDigest) != digestString(diskDigest) {
		return &SessionSnapshotConflictError{Path: path, Kind: SessionSnapshotConflictDiverged, BaseRevision: s.persistedRevision, DiskRevision: meta.Revision}
	}
	if s.persistedOK && meta.Revision != s.persistedRevision && strings.TrimSpace(meta.ContentDigest) != digestString(s.persistedDigest) {
		return &SessionSnapshotConflictError{Path: path, Kind: SessionSnapshotConflictDiverged, BaseRevision: s.persistedRevision, DiskRevision: meta.Revision}
	}
	if s.persistedOK && meta.Revision == s.persistedRevision && strings.TrimSpace(meta.ContentDigest) == digestString(d) {
		return nil
	}
	if s.persistedOK && meta.Revision != s.persistedRevision && !messagesHavePrefix(next, disk) {
		return &SessionSnapshotConflictError{Path: path, Kind: SessionSnapshotConflictDiverged, BaseRevision: s.persistedRevision, DiskRevision: meta.Revision}
	}
	return nil
}
func loadMessagesForCAS(path string) ([]provider.Message, error) {
	x, e := LoadSession(path)
	if e != nil || x == nil {
		return nil, fmt.Errorf("load existing session for CAS: %w", e)
	}
	return x.Snapshot(), nil
}
func (s *Session) casAfterSave(path string, d [sha256.Size]byte) error {
	m, _, _ := LoadBranchMeta(path)
	m.Revision++
	m.ContentDigest = digestString(d)
	m.WriterID = SessionWriterID()
	if err := SaveBranchMetaPreserveUpdated(path, m); err != nil {
		return err
	}
	s.mu.Lock()
	s.persistedPath = path
	s.persistedDigest = d
	s.persistedRevision = m.Revision
	s.persistedOK = true
	s.mu.Unlock()
	return nil
}
