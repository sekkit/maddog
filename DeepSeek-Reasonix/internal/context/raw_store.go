package context

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const rawStoreScheme = "raw://tool-output/"

var rawStoreNamePattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type RawRecord struct {
	Ref string
}

type RawStore interface {
	Put(ToolOutput) (RawRecord, error)
	Get(ref string) (string, error)
}

type FileRawStore struct {
	root string
}

func NewFileRawStore(root string) *FileRawStore {
	return &FileRawStore{root: strings.TrimSpace(root)}
}

func (s *FileRawStore) Put(in ToolOutput) (RawRecord, error) {
	if s == nil || s.root == "" {
		return RawRecord{}, fmt.Errorf("raw store is not configured")
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return RawRecord{}, err
	}
	sum := sha256.Sum256([]byte(in.Tool + "\x00" + in.CallID + "\x00" + in.Output))
	name := safeRawStoreName(in.CallID)
	if name == "" {
		name = safeRawStoreName(in.Tool)
	}
	if name == "" {
		name = "tool"
	}
	name = name + "-" + hex.EncodeToString(sum[:8]) + ".txt"
	if err := os.WriteFile(filepath.Join(s.root, name), []byte(in.Output), 0o600); err != nil {
		return RawRecord{}, err
	}
	return RawRecord{Ref: rawStoreScheme + name}, nil
}

func (s *FileRawStore) Get(ref string) (string, error) {
	if s == nil || s.root == "" {
		return "", fmt.Errorf("raw store is not configured")
	}
	name, ok := strings.CutPrefix(strings.TrimSpace(ref), rawStoreScheme)
	if !ok || name == "" || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid raw ref")
	}
	b, err := os.ReadFile(filepath.Join(s.root, name))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func safeRawStoreName(name string) string {
	name = rawStoreNamePattern.ReplaceAllString(strings.TrimSpace(name), "-")
	name = strings.Trim(name, ".-_")
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}
