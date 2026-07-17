// Package mcptrust contains Maddog's local trust primitives for MCP capabilities.
// It deliberately has no knowledge of transports or UI: callers supply the
// discovered identity/capability snapshot and revalidate it at dispatch time.
package mcptrust

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	// ErrUntrusted reports that no valid trust decision covers a capability.
	ErrUntrusted = errors.New("MCP capability is not trusted")
	// ErrRevoked reports that a persisted approval is revoked or expired.
	ErrRevoked = errors.New("MCP capability receipt is revoked or expired")
	// ErrCapabilityDrift reports that a tool changed after it was approved.
	ErrCapabilityDrift = errors.New("MCP capability changed since approval")
)

// Identity is the transport-level identity of one MCP server.
type Identity struct {
	Name, Type, Command, URL string
	Args                     []string
}

// Capability is the security-relevant snapshot of one MCP tool.
type Capability struct {
	Server, Tool          string
	Schema                []byte
	ReadOnly, Destructive bool
}

// IdentityFingerprint returns a credential-free stable identity digest.
func IdentityFingerprint(i Identity) string {
	u := credentialSafeURL(i.URL)
	typ := strings.ToLower(strings.TrimSpace(i.Type))
	switch typ {
	case "":
		typ = "stdio"
	case "streamable-http", "streamable_http":
		typ = "http"
	}
	v := struct {
		Name, Type, Command, URL string
		Args                     []string
	}{i.Name, typ, i.Command, u, append([]string(nil), i.Args...)}
	return digest(v)
}

// CapabilityFingerprint returns a stable digest of a tool's security surface.
func CapabilityFingerprint(c Capability) string {
	v := struct {
		Server, Tool, Schema  string
		ReadOnly, Destructive bool
	}{c.Server, c.Tool, string(c.Schema), c.ReadOnly, c.Destructive}
	return digest(v)
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func credentialSafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// Receipt records a persisted human approval for one identity and capability.
type Receipt struct {
	Identity, Capability string
	Approved             bool
	ExpiresAt            time.Time
	Revision             uint64
}

// Store persists and revokes capability receipts.
type Store interface {
	Put(context.Context, Receipt) error
	Get(context.Context, string, string) (Receipt, error)
	Revoke(context.Context, string, string) error
}

// MemoryStore is an in-memory receipt store for ephemeral runtimes and tests.
type MemoryStore struct {
	mu sync.RWMutex
	m  map[string]Receipt
}

// FileStore persists receipts in one private JSON file. Writes use a sibling
// temporary file and rename, so a crash cannot leave a partially-written trust
// record. Sequence numbers are monotonic and reject rollback/replay.
type FileStore struct {
	mu   sync.Mutex
	Path string
}
type fileReceipts struct {
	Sequence uint64    `json:"sequence"`
	Receipts []Receipt `json:"receipts"`
}

// NewFileStore returns a private JSON receipt store backed by path.
func NewFileStore(path string) *FileStore { return &FileStore{Path: path} }
func (s *FileStore) load() (fileReceipts, error) {
	b, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return fileReceipts{}, nil
	}
	if err != nil {
		return fileReceipts{}, err
	}
	var f fileReceipts
	if err = json.Unmarshal(b, &f); err != nil {
		return fileReceipts{}, fmt.Errorf("decode receipt store: %w", err)
	}
	return f, nil
}

// Put persists a receipt while rejecting revision rollback.
func (s *FileStore) Put(ctx context.Context, r Receipt) error {
	_ = ctx
	if r.Identity == "" || r.Capability == "" {
		return errors.New("receipt identity and capability are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	for _, old := range f.Receipts {
		if old.Identity == r.Identity && old.Capability == r.Capability && r.Revision < old.Revision {
			return errors.New("receipt revision rollback")
		}
	}
	if r.Revision <= f.Sequence {
		r.Revision = f.Sequence + 1
	}
	f.Sequence = r.Revision
	replaced := false
	for i := range f.Receipts {
		if f.Receipts[i].Identity == r.Identity && f.Receipts[i].Capability == r.Capability {
			f.Receipts[i] = r
			replaced = true
		}
	}
	if !replaced {
		f.Receipts = append(f.Receipts, r)
	}
	return s.save(f)
}

// Get returns a currently approved, unexpired receipt.
func (s *FileStore) Get(ctx context.Context, i, c string) (Receipt, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return Receipt{}, err
	}
	for _, r := range f.Receipts {
		if r.Identity == i && r.Capability == c {
			if !r.Approved {
				return Receipt{}, ErrRevoked
			}
			if !r.ExpiresAt.IsZero() && !time.Now().Before(r.ExpiresAt) {
				return Receipt{}, ErrRevoked
			}
			return r, nil
		}
	}
	return Receipt{}, ErrUntrusted
}

// Revoke durably revokes a receipt and advances the store sequence.
func (s *FileStore) Revoke(ctx context.Context, identity, capability string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	for n := range f.Receipts {
		if f.Receipts[n].Identity == identity && f.Receipts[n].Capability == capability {
			f.Sequence++
			f.Receipts[n].Approved = false
			f.Receipts[n].Revision = f.Sequence
			return s.save(f)
		}
	}
	return ErrUntrusted
}
func (s *FileStore) save(f fileReceipts) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	b, _ := json.Marshal(f)
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// MigrateLegacy imports a legacy receipt file at most once. The marker is
// written only after every converted receipt is durable; subsequent starts do
// not silently re-import a legacy file recreated by old software.
func MigrateLegacy(ctx context.Context, legacyPath, markerPath string, decode func([]byte) ([]Receipt, error), dst Store) error {
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	}
	b, err := os.ReadFile(legacyPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if decode == nil || dst == nil {
		return errors.New("legacy receipt migration requires decoder and destination")
	}
	receipts, err := decode(b)
	if err != nil {
		return err
	}
	for _, r := range receipts {
		if err := dst.Put(ctx, r); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0700); err != nil {
		return err
	}
	return os.WriteFile(markerPath, []byte("migrated\n"), 0600)
}

// NewMemoryStore returns an empty in-memory receipt store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{m: make(map[string]Receipt)} }
func key(i, c string) string       { return i + "\x00" + c }

// Put stores a receipt in memory.
func (s *MemoryStore) Put(_ context.Context, r Receipt) error {
	if r.Identity == "" || r.Capability == "" {
		return errors.New("receipt identity and capability are required")
	}
	s.mu.Lock()
	s.m[key(r.Identity, r.Capability)] = r
	s.mu.Unlock()
	return nil
}

// Get returns a currently approved, unexpired in-memory receipt.
func (s *MemoryStore) Get(_ context.Context, i, c string) (Receipt, error) {
	s.mu.RLock()
	r, ok := s.m[key(i, c)]
	s.mu.RUnlock()
	if !ok || !r.Approved {
		return Receipt{}, ErrUntrusted
	}
	if !r.ExpiresAt.IsZero() && !time.Now().Before(r.ExpiresAt) {
		return Receipt{}, ErrRevoked
	}
	return r, nil
}

// Revoke revokes an in-memory receipt.
func (s *MemoryStore) Revoke(_ context.Context, i, c string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[key(i, c)]
	if !ok {
		return ErrUntrusted
	}
	r.Approved = false
	s.m[key(i, c)] = r
	return nil
}

// Authority is intentionally a narrow seam so dispatch can revalidate without
// knowing how Maddog persists or signs receipts.
type Authority interface {
	Check(context.Context, Identity, Capability) error
}

// ReceiptAuthority validates capabilities against persisted human approvals.
type ReceiptAuthority struct{ Store Store }

// Check validates an identity and capability against the receipt store.
func (a ReceiptAuthority) Check(ctx context.Context, i Identity, c Capability) error {
	if a.Store == nil {
		return ErrUntrusted
	}
	_, err := a.Store.Get(ctx, IdentityFingerprint(i), CapabilityFingerprint(c))
	return err
}

// CheckRedirectOrigin rejects redirects that change URL scheme or authority.
func CheckRedirectOrigin(origin, next *url.URL) error {
	if origin == nil || next == nil || !strings.EqualFold(origin.Scheme, next.Scheme) || !strings.EqualFold(origin.Host, next.Host) {
		return fmt.Errorf("MCP redirect changes origin")
	}
	return nil
}

// CredentialSafeHTTPClient returns a client that refuses cross-origin redirects.
func CredentialSafeHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}
		return CheckRedirectOrigin(via[0].URL, req.URL)
	}}
}
