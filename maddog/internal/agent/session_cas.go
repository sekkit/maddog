package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"maddog/internal/fileutil"
	"maddog/internal/provider"
)

// MaxRecoveryBranches is the maximum number of recovery transcripts retained
// beside one canonical session. The newest distinct snapshots win.
const MaxRecoveryBranches = 5

var recoveryBranchMu sync.Mutex

// RecoveryBranchOptions identifies the canonical transcript and why the
// in-memory snapshot could not safely replace it.
type RecoveryBranchOptions struct {
	OriginalPath string
	Reason       string
}

// RecoveryBranchInfo describes a durable recovery transcript and its sidecar.
type RecoveryBranchInfo struct {
	Path   string
	Digest string
	Meta   BranchMeta
}

// SaveRecoveryBranch preserves a conflicting snapshot without overwriting the
// canonical transcript. Identical snapshots are deduplicated and only the
// newest MaxRecoveryBranches distinct snapshots are retained.
func (s *Session) SaveRecoveryBranch(opts RecoveryBranchOptions) (RecoveryBranchInfo, error) {
	if strings.TrimSpace(opts.OriginalPath) == "" {
		return RecoveryBranchInfo{}, fmt.Errorf("empty original session path")
	}
	msgs := s.Snapshot()
	d, err := digestSessionMessages(msgs)
	if err != nil {
		return RecoveryBranchInfo{}, err
	}
	digest := digestString(d)
	recoveryBranchMu.Lock()
	defer recoveryBranchMu.Unlock()
	if err := pruneRecoveryBranches(opts.OriginalPath, MaxRecoveryBranches); err != nil {
		return RecoveryBranchInfo{}, err
	}
	if info, ok := findRecoveryBranch(opts.OriginalPath, digest); ok {
		return info, nil
	}
	base := strings.TrimSuffix(opts.OriginalPath, filepath.Ext(opts.OriginalPath))
	path := fmt.Sprintf("%s.recovery-%d.jsonl", base, time.Now().UTC().UnixNano())
	if err := s.writeRecovery(path, msgs); err != nil {
		return RecoveryBranchInfo{}, err
	}
	meta := BranchMeta{ID: BranchID(path), ParentID: BranchID(opts.OriginalPath), Recovered: true, RecoveryReason: opts.Reason, RecoveryDigest: digest, Revision: 1, ContentDigest: digest, WriterID: SessionWriterID(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := SaveBranchMeta(path, meta); err != nil {
		_ = os.Remove(path)
		return RecoveryBranchInfo{}, err
	}
	info := RecoveryBranchInfo{Path: path, Digest: digest, Meta: meta}
	if err := pruneRecoveryBranches(opts.OriginalPath, MaxRecoveryBranches); err != nil {
		return info, err
	}
	return info, nil
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
	return fileutil.AtomicWriteFile(path, b, 0o600)
}

type recoveryFile struct {
	path    string
	modTime time.Time
}

func recoveryFiles(originalPath string) ([]recoveryFile, error) {
	dir := filepath.Dir(originalPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	base := strings.TrimSuffix(filepath.Base(originalPath), filepath.Ext(originalPath)) + ".recovery-"
	var out []recoveryFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), base) || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, recoveryFile{path: filepath.Join(dir, entry.Name()), modTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].modTime.Equal(out[j].modTime) {
			return out[i].path < out[j].path
		}
		return out[i].modTime.Before(out[j].modTime)
	})
	return out, nil
}

func findRecoveryBranch(originalPath, digest string) (RecoveryBranchInfo, bool) {
	files, err := recoveryFiles(originalPath)
	if err != nil {
		return RecoveryBranchInfo{}, false
	}
	for i := len(files) - 1; i >= 0; i-- {
		meta, ok, err := LoadBranchMeta(files[i].path)
		if err != nil || !ok || meta.RecoveryDigest != digest {
			continue
		}
		session, err := LoadSession(files[i].path)
		if err != nil {
			continue
		}
		actual, err := digestSessionMessages(session.Snapshot())
		if err == nil && digestString(actual) == digest {
			return RecoveryBranchInfo{Path: files[i].path, Digest: digest, Meta: meta}, true
		}
	}
	return RecoveryBranchInfo{}, false
}

func pruneRecoveryBranches(originalPath string, keep int) error {
	files, err := recoveryFiles(originalPath)
	if err != nil {
		return err
	}
	if keep < 0 {
		keep = 0
	}
	for _, file := range files[:max(0, len(files)-keep)] {
		if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Remove(BranchMetaPath(file.path)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
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
