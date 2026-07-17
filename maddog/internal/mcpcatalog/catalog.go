// Package mcpcatalog defines the Maddog-signed, local candidate catalog seam.
// Reasonix metadata can be imported as candidates, but only this package's
// verification result is suitable for an Maddog trust authority.
package mcpcatalog

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"maddog/internal/mcptrust"
)

// ProductionPublicKeyHex is Maddog's Ed25519 catalog verification key. It is
// the raw Ed25519 key already owned by Maddog's minisign release identity; the
// matching private key remains only in the release signing secret.
const ProductionPublicKeyHex = "a66187aaade583bde5892909ac933823055433544128c9630c6862780d5414aa"

// Catalog is a versioned set of Maddog-approved MCP identities and tool pins.
type Catalog struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Entry pins one MCP transport identity and its approved tool capabilities.
type Entry struct {
	Name, Type, Command string
	Args                []string
	URL                 string
	CapabilityPins      map[string]string `json:"capability_pins,omitempty"`
}

// Signed wraps a catalog with its detached Ed25519 signature.
type Signed struct {
	Payload   Catalog `json:"payload"`
	Signature []byte  `json:"signature"`
}

// Pin is an exact stdio command and argument vector.
type Pin struct {
	Command string
	Args    []string
}

// FileStore persists one signed catalog with rollback protection.
type FileStore struct {
	mu   sync.Mutex
	Path string
}

// NewFileStore returns a catalog store backed by path.
func NewFileStore(path string) *FileStore { return &FileStore{Path: path} }

// Save atomically persists a signed catalog unless its version rolls back.
func (s *FileStore) Save(c Signed) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var old Signed
	if b, err := os.ReadFile(s.Path); err == nil {
		if json.Unmarshal(b, &old) == nil && c.Payload.Version < old.Payload.Version {
			return errors.New("catalog version rollback")
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	b, _ := json.Marshal(c)
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Load reads and verifies the persisted catalog with key.
func (s *FileStore) Load(key ed25519.PublicKey) (Signed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return Signed{}, err
	}
	var c Signed
	if err = json.Unmarshal(b, &c); err != nil {
		return Signed{}, err
	}
	if err := Verify(c, key); err != nil {
		return Signed{}, err
	}
	return c, nil
}

func canonical(c Catalog) []byte { b, _ := json.Marshal(c); return b }

// Sign signs the canonical catalog payload with a Maddog-owned Ed25519 key.
func Sign(c Catalog, key ed25519.PrivateKey) Signed {
	return Signed{Payload: c, Signature: ed25519.Sign(key, canonical(c))}
}

// Verify checks a signed catalog against key.
func Verify(s Signed, key ed25519.PublicKey) error {
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, canonical(s.Payload), s.Signature) {
		return errors.New("invalid Maddog catalog signature")
	}
	return nil
}

// Matches reports whether command and args exactly match the pin.
func (p Pin) Matches(command string, args []string) bool {
	if p.Command != command || len(p.Args) != len(args) {
		return false
	}
	for i := range args {
		if p.Args[i] != args[i] {
			return false
		}
	}
	return true
}

// EntryFingerprint returns the canonical SHA-256 fingerprint for entry.
func EntryFingerprint(e Entry) string {
	b := canonical(Catalog{Version: 1, Entries: []Entry{e}})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Authority re-reads and verifies the signed catalog for every trust decision.
// Keeping the key and store on the immutable plugin spec prevents a runtime
// configuration source from becoming Maddog's trust root.
type Authority struct {
	Store     *FileStore
	PublicKey ed25519.PublicKey
}

// ProductionPublicKey returns Maddog's embedded catalog verification key.
func ProductionPublicKey() ed25519.PublicKey {
	b, err := hex.DecodeString(ProductionPublicKeyHex)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}

// Check revalidates identity and capability against the current signed catalog.
func (a Authority) Check(ctx context.Context, identity mcptrust.Identity, capability mcptrust.Capability) error {
	_ = ctx
	if a.Store == nil || len(a.PublicKey) != ed25519.PublicKeySize {
		return mcptrust.ErrUntrusted
	}
	signed, err := a.Store.Load(a.PublicKey)
	if err != nil {
		return fmt.Errorf("load Maddog MCP catalog: %w", err)
	}
	for _, entry := range signed.Payload.Entries {
		candidate := mcptrust.Identity{Name: entry.Name, Type: entry.Type, Command: entry.Command, URL: entry.URL, Args: entry.Args}
		if mcptrust.IdentityFingerprint(candidate) != mcptrust.IdentityFingerprint(identity) {
			continue
		}
		if strings.TrimSpace(capability.Tool) == "" {
			return nil
		}
		pin, ok := entry.CapabilityPins[capability.Tool]
		if !ok || !strings.EqualFold(pin, mcptrust.CapabilityFingerprint(capability)) {
			return mcptrust.ErrCapabilityDrift
		}
		return nil
	}
	return mcptrust.ErrUntrusted
}
