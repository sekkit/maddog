// Package worktree manages explicit, branch-backed delivery worktrees.
// Nothing in this package is automatic: callers must explicitly create, apply,
// or discard a delivery. State is kept outside the repository so crash recovery
// can reopen the branch and preserve user changes.
package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"maddog/internal/fileutil"
	"maddog/internal/processlock"
)

var (
	// ErrLeaseHeld reports that another live process is mutating the delivery workspace.
	ErrLeaseHeld = errors.New("delivery workspace is already leased")
	// ErrNotFound reports that the requested delivery does not exist or was discarded.
	ErrNotFound = errors.New("delivery workspace not found")
	// ErrNotReady reports that the requested lifecycle action is invalid for the delivery state.
	ErrNotReady = errors.New("delivery workspace is not ready")
)

// DeliveryState identifies a persisted delivery lifecycle state.
type DeliveryState string

const (
	// StateOpen identifies a delivery that can still be inspected, applied, or discarded.
	StateOpen DeliveryState = "open"
	// StateApplied identifies a delivery that has been merged into its base workspace.
	StateApplied DeliveryState = "applied"
	// StateDiscarded identifies a delivery whose worktree has been explicitly removed.
	StateDiscarded DeliveryState = "discarded"
)

// Delivery is the persisted lifecycle record for one branch-backed worktree.
type Delivery struct {
	ID           string        `json:"id"`
	BasePath     string        `json:"basePath"`
	Path         string        `json:"path"`
	Branch       string        `json:"branch"`
	BaseRevision string        `json:"baseRevision"`
	State        DeliveryState `json:"state"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
}

// Inspection describes the current Git readiness and changed files of a delivery.
type Inspection struct {
	Delivery Delivery `json:"delivery"`
	Head     string   `json:"head"`
	Dirty    bool     `json:"dirty"`
	Files    []string `json:"files"`
	Ready    bool     `json:"ready"`
}

// Manager owns explicit delivery lifecycle operations for one base workspace.
type Manager struct {
	base, state string
	mu          sync.Mutex
}

// List returns persisted deliveries, including applied deliveries. Discarded
// entries are retained as tombstones so a crash or a repeated discard cannot
// accidentally recreate a branch that the user explicitly removed.
func (m *Manager) List() ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(m.state, "deliveries"))
	if err != nil {
		return nil, err
	}
	out := make([]Delivery, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(m.state, "deliveries", entry.Name()))
		if err != nil {
			return nil, err
		}
		var d Delivery
		if err := json.Unmarshal(b, &d); err != nil {
			return nil, fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		if err := validateDeliveryState(d.State); err != nil {
			return nil, fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		out = append(out, d)
	}
	return out, nil
}

// NewManager creates a delivery manager rooted at basePath with Maddog-owned state.
func NewManager(basePath, statePath string) (*Manager, error) {
	base, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("resolve base workspace path: %w", err)
	}
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("base workspace: %w", err)
	}
	state, err := filepath.Abs(statePath)
	if err != nil {
		return nil, fmt.Errorf("resolve delivery state path: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(state, "deliveries"), 0o700); err != nil {
		return nil, err
	}
	return &Manager{base: base, state: state}, nil
}

// Create creates an isolated branch-backed delivery worktree after taking the base lease.
func (m *Manager) Create(ctx context.Context, branch string) (Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lease, err := acquireLease(m.state, "base")
	if err != nil {
		return Delivery{}, err
	}
	defer lease.Release()
	if err := m.ensureGit(ctx); err != nil {
		return Delivery{}, err
	}
	if strings.TrimSpace(branch) == "" {
		branch = "maddog/delivery/" + shortHash(time.Now().UTC().String())
	}
	baseRev, err := m.git(ctx, "-C", m.base, "rev-parse", "HEAD")
	if err != nil {
		return Delivery{}, err
	}
	id := shortHash(branch + "\x00" + time.Now().UTC().Format(time.RFC3339Nano))
	path := ShortPath(m.state, id)
	if !within(m.state, path) {
		return Delivery{}, fmt.Errorf("delivery path escapes state root")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Delivery{}, err
	}
	if _, err := m.git(ctx, "-C", m.base, "worktree", "add", "-b", branch, path, strings.TrimSpace(baseRev)); err != nil {
		return Delivery{}, err
	}
	now := time.Now().UTC()
	d := Delivery{ID: id, BasePath: m.base, Path: path, Branch: branch, BaseRevision: strings.TrimSpace(baseRev), State: StateOpen, CreatedAt: now, UpdatedAt: now}
	if err := m.save(d); err != nil {
		_ = m.removeWorktree(ctx, path)
		return Delivery{}, err
	}
	return d, nil
}

// Open validates and returns a persisted delivery without changing repository state.
func (m *Manager) Open(id string) (Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lease, err := acquireLease(m.state, id)
	if err != nil {
		return Delivery{}, err
	}
	defer lease.Release()
	d, err := m.load(id)
	if err != nil {
		return Delivery{}, err
	}
	if d.State == StateDiscarded {
		return Delivery{}, ErrNotFound
	}
	if !within(m.state, d.Path) {
		return Delivery{}, fmt.Errorf("delivery path escapes state root")
	}
	if _, err := os.Stat(d.Path); err != nil {
		return Delivery{}, fmt.Errorf("delivery path unavailable: %w", err)
	}
	d.UpdatedAt = time.Now().UTC()
	if err := m.save(d); err != nil {
		return Delivery{}, err
	}
	return d, nil
}

// Inspect reads Git readiness and changed files without mutating the delivery.
func (m *Manager) Inspect(ctx context.Context, id string) (Inspection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lease, err := acquireLease(m.state, id)
	if err != nil {
		return Inspection{}, err
	}
	defer lease.Release()
	d, err := m.load(id)
	if err != nil {
		return Inspection{}, err
	}
	if d.State != StateOpen && d.State != StateApplied {
		return Inspection{}, ErrNotReady
	}
	if !within(m.state, d.Path) {
		return Inspection{}, fmt.Errorf("delivery path escapes state root")
	}
	head, err := m.git(ctx, "-C", d.Path, "rev-parse", "HEAD")
	if err != nil {
		return Inspection{}, err
	}
	status, err := m.git(ctx, "-C", d.Path, "status", "--porcelain")
	if err != nil {
		return Inspection{}, err
	}
	files := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(status), "\n") {
		if strings.TrimSpace(line) != "" {
			files = append(files, strings.TrimSpace(line))
		}
	}
	return Inspection{Delivery: d, Head: strings.TrimSpace(head), Dirty: len(files) > 0, Files: files, Ready: true}, nil
}

// Apply is intentionally explicit. It fast-forwards the base branch only;
// conflicts are surfaced to the caller and the delivery remains recoverable.
func (m *Manager) Apply(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.load(id)
	if err != nil {
		return err
	}
	if d.State != StateOpen {
		return ErrNotReady
	}
	lease, err := acquireLease(m.state, "base")
	if err != nil {
		return err
	}
	defer lease.Release()
	deliveryLease, err := acquireLease(m.state, id)
	if err != nil {
		return err
	}
	defer deliveryLease.Release()
	status, err := m.git(ctx, "-C", d.Path, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("delivery has uncommitted changes")
	}
	baseStatus, err := m.git(ctx, "-C", m.base, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(baseStatus) != "" {
		return fmt.Errorf("base workspace has uncommitted changes")
	}
	if _, err := m.git(ctx, "-C", m.base, "merge", "--ff-only", d.Branch); err != nil {
		return err
	}
	d.State = StateApplied
	d.UpdatedAt = time.Now().UTC()
	return m.save(d)
}

// Discard explicitly removes a delivery worktree and persists a tombstone.
func (m *Manager) Discard(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.load(id)
	if err != nil {
		return err
	}
	if d.State == StateDiscarded {
		return nil
	}
	baseLease, err := acquireLease(m.state, "base")
	if err != nil {
		return err
	}
	defer baseLease.Release()
	deliveryLease, err := acquireLease(m.state, id)
	if err != nil {
		return err
	}
	defer deliveryLease.Release()
	if err := m.removeWorktree(ctx, d.Path); err != nil {
		return err
	}
	d.State = StateDiscarded
	d.UpdatedAt = time.Now().UTC()
	return m.save(d)
}

func (m *Manager) load(id string) (Delivery, error) {
	if strings.TrimSpace(id) == "" {
		return Delivery{}, ErrNotFound
	}
	b, err := os.ReadFile(filepath.Join(m.state, "deliveries", id+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Delivery{}, ErrNotFound
		}
		return Delivery{}, err
	}
	var d Delivery
	if err := json.Unmarshal(b, &d); err != nil {
		return Delivery{}, err
	}
	if err := validateDeliveryState(d.State); err != nil {
		return Delivery{}, err
	}
	return d, nil
}
func (m *Manager) save(d Delivery) error {
	if err := validateDeliveryState(d.State); err != nil {
		return err
	}
	if d.ID == "" || !within(m.state, d.Path) {
		return fmt.Errorf("invalid delivery state path")
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(m.state, "deliveries", d.ID+".json")
	return fileutil.AtomicWriteFile(p, b, 0o600)
}

func validateDeliveryState(state DeliveryState) error {
	switch state {
	case StateOpen, StateApplied, StateDiscarded:
		return nil
	default:
		return fmt.Errorf("%w: unknown delivery state %q", ErrNotReady, state)
	}
}
func (m *Manager) ensureGit(ctx context.Context) error {
	_, err := m.git(ctx, "-C", m.base, "rev-parse", "--git-dir")
	return err
}
func (m *Manager) git(ctx context.Context, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "git", args...)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
func (m *Manager) removeWorktree(ctx context.Context, p string) error {
	_, err := m.git(ctx, "-C", m.base, "worktree", "remove", "--force", p)
	return err
}

func shortHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:])[:12] }

func within(root, path string) bool {
	r, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(r, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func acquireLease(root, name string) (*processlock.Lock, error) {
	p := filepath.Join(root, "leases", shortHash(name)+".lock")
	lock, acquired, err := processlock.Try(p)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, ErrLeaseHeld
	}
	return lock, nil
}

// ShortPath returns a Maddog-owned short path suitable for Windows worktrees.
func ShortPath(stateRoot, identity string) string {
	if runtime.GOOS != "windows" {
		return filepath.Join(stateRoot, "deliveries", shortHash(identity))
	}
	return filepath.Join(stateRoot, "wt", shortHash(identity))
}
