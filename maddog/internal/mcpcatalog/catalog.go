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

// ProductionPublicKeyHex is replaced with Maddog's Ed25519 catalog public key
// by the release process. An empty value deliberately disables official trust.
const ProductionPublicKeyHex = ""

type Catalog struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}
type Entry struct {
	Name, Type, Command string
	Args                []string
	URL                 string
	CapabilityPins      map[string]string `json:"capability_pins,omitempty"`
}
type Signed struct {
	Payload   Catalog `json:"payload"`
	Signature []byte  `json:"signature"`
}
type Pin struct {
	Command string
	Args    []string
}

type FileStore struct {
	mu   sync.Mutex
	Path string
}

func NewFileStore(path string) *FileStore { return &FileStore{Path: path} }
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
func Sign(c Catalog, key ed25519.PrivateKey) Signed {
	return Signed{Payload: c, Signature: ed25519.Sign(key, canonical(c))}
}
func Verify(s Signed, key ed25519.PublicKey) error {
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, canonical(s.Payload), s.Signature) {
		return errors.New("invalid Maddog catalog signature")
	}
	return nil
}
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

func ProductionPublicKey() ed25519.PublicKey {
	b, err := hex.DecodeString(ProductionPublicKeyHex)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}

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
