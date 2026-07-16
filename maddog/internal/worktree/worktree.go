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
)

var (
	ErrLeaseHeld = errors.New("delivery workspace is already leased")
	ErrNotFound  = errors.New("delivery workspace not found")
	ErrNotReady  = errors.New("delivery workspace is not ready")
)

type Delivery struct {
	ID           string    `json:"id"`
	BasePath     string    `json:"basePath"`
	Path         string    `json:"path"`
	Branch       string    `json:"branch"`
	BaseRevision string    `json:"baseRevision"`
	State        string    `json:"state"` // open, applied, discarded
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Inspection struct {
	Delivery Delivery `json:"delivery"`
	Head     string   `json:"head"`
	Dirty    bool     `json:"dirty"`
	Files    []string `json:"files"`
	Ready    bool     `json:"ready"`
}

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
		out = append(out, d)
	}
	return out, nil
}

func NewManager(basePath, statePath string) (*Manager, error) {
	base, err := filepath.Abs(basePath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("base workspace: %w", err)
	}
	state, err := filepath.Abs(statePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(state, "deliveries"), 0o700); err != nil {
		return nil, err
	}
	return &Manager{base: base, state: state}, nil
}

func (m *Manager) Create(ctx context.Context, branch string) (Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := acquireLease(m.state, "base"); err != nil {
		return Delivery{}, err
	}
	defer releaseLease(m.state, "base")
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
	d := Delivery{ID: id, BasePath: m.base, Path: path, Branch: branch, BaseRevision: strings.TrimSpace(baseRev), State: "open", CreatedAt: now, UpdatedAt: now}
	if err := m.save(d); err != nil {
		_ = m.removeWorktree(ctx, path)
		return Delivery{}, err
	}
	return d, nil
}

func (m *Manager) Open(id string) (Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.load(id)
	if err != nil {
		return Delivery{}, err
	}
	if d.State == "discarded" {
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

func (m *Manager) Inspect(ctx context.Context, id string) (Inspection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.load(id)
	if err != nil {
		return Inspection{}, err
	}
	if d.State != "open" && d.State != "applied" {
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
	if d.State != "open" {
		return ErrNotReady
	}
	if err := acquireLease(m.state, "base"); err != nil {
		return err
	}
	defer releaseLease(m.state, "base")
	status, err := m.git(ctx, "-C", d.Path, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("delivery has uncommitted changes")
	}
	if _, err := m.git(ctx, "-C", m.base, "merge", "--ff-only", d.Branch); err != nil {
		return err
	}
	d.State = "applied"
	d.UpdatedAt = time.Now().UTC()
	return m.save(d)
}

func (m *Manager) Discard(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.load(id)
	if err != nil {
		return err
	}
	if d.State == "discarded" {
		return nil
	}
	if err := acquireLease(m.state, id); err != nil {
		return err
	}
	defer releaseLease(m.state, id)
	if err := m.removeWorktree(ctx, d.Path); err != nil {
		return err
	}
	d.State = "discarded"
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
	return d, nil
}
func (m *Manager) save(d Delivery) error {
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
func acquireLease(root, name string) error {
	p := filepath.Join(root, "leases", shortHash(name)+".lock")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return ErrLeaseHeld
		}
		return err
	}
	_, _ = fmt.Fprintf(f, "pid=%d\ntime=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	_ = f.Close()
	return nil
}
func releaseLease(root, name string) {
	_ = os.Remove(filepath.Join(root, "leases", shortHash(name)+".lock"))
}

// ShortPath returns a Maddog-owned short path suitable for Windows worktrees.
func ShortPath(stateRoot, identity string) string {
	if runtime.GOOS != "windows" {
		return filepath.Join(stateRoot, "deliveries", shortHash(identity))
	}
	return filepath.Join(stateRoot, "wt", shortHash(identity))
}
