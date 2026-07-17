package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aead.dev/minisign"

	"maddog/desktop/internal/update"
	"maddog/internal/mcpcatalog"
)

func TestCatalogProductionKeyUsesMaddogReleaseIdentity(t *testing.T) {
	parts := strings.SplitN(update.PublicKey(), "\n", 2)
	if len(parts) != 2 {
		t.Fatal("embedded Maddog release public key is malformed")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
	if err != nil || len(raw) != 10+ed25519.PublicKeySize {
		t.Fatalf("decode release public key: len=%d err=%v", len(raw), err)
	}
	want := ed25519.PublicKey(raw[10:])
	if !want.Equal(mcpcatalog.ProductionPublicKey()) {
		t.Fatal("MCP catalog key does not match Maddog's release signing identity")
	}
}

func TestCatalogPrivateKeyExtraction(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edKey, err := catalogPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(pub.String())
	if err != nil || len(raw) != 10+ed25519.PublicKeySize {
		t.Fatalf("decode generated public key: len=%d err=%v", len(raw), err)
	}
	if !edKey.Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(raw[10:])) {
		t.Fatal("extracted private key does not match the minisign public key")
	}
	message := []byte("catalog")
	sig := ed25519.Sign(edKey, message)
	if !ed25519.Verify(edKey.Public().(ed25519.PublicKey), message, sig) {
		t.Fatal("extracted catalog key cannot sign a standard Ed25519 payload")
	}
}

// TestSignFiles signs a file with a throwaway key pair (injected via env, exactly
// as CI passes the real key) and verifies the produced .minisig validates under the
// matching public key.
func TestSignFiles(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := minisign.EncryptKey("pw", priv)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINISIGN_PRIVATE_KEY", string(enc))
	t.Setenv("MINISIGN_PASSWORD", "pw")

	dir := t.TempDir()
	artifact := filepath.Join(dir, "Maddog-linux-amd64.tar.gz")
	payload := []byte("pretend this is a release tarball")
	if err := os.WriteFile(artifact, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := signFiles([]string{artifact}); err != nil {
		t.Fatalf("signFiles: %v", err)
	}
	sig, err := os.ReadFile(artifact + ".minisig")
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	if !minisign.Verify(pub, payload, sig) {
		t.Fatal("produced signature does not verify under the signing key")
	}
}

// TestGenManifest builds a manifest from a directory of fake artifacts and checks
// every platform is listed with a download URL, a parallel .minisig URL, and a
// non-empty digest. The .minisig and latest.json files must be ignored.
func TestGenManifest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"Maddog-darwin-arm64.zip",
		"Maddog-darwin-amd64.zip",
		"Maddog-windows-amd64-installer.exe",
		"Maddog-windows-arm64-installer.exe",
		"Maddog-windows-amd64.zip", // portable download, not the updater channel
		"Maddog-windows-arm64.zip", // portable download, not the updater channel
		"Maddog-linux-amd64-update.tar.gz",
		"Maddog-linux-amd64.tar.gz",                // guarded human download, not updater channel
		"Maddog-linux-amd64.deb",                   // human download, not the updater channel
		"Maddog-linux-amd64-update.tar.gz.minisig", // must be skipped
		"README.txt",                               // unmatched, must be skipped
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GITHUB_REPOSITORY", "sekkit/maddog")

	if err := genManifest(dir, "v1.2.0", "desktop-v1.2.0"); err != nil {
		t.Fatalf("genManifest: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m update.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("latest.json is not valid: %v", err)
	}
	if m.Version != "v1.2.0" {
		t.Fatalf("version = %q, want v1.2.0", m.Version)
	}
	if len(m.Platforms) != 5 {
		t.Fatalf("want 5 platforms, got %d: %v", len(m.Platforms), m.Platforms)
	}
	win, ok := m.Platforms["windows-amd64"]
	if !ok {
		t.Fatal("windows-amd64 missing")
	}
	wantURL := "https://github.com/sekkit/maddog/releases/download/desktop-v1.2.0/Maddog-windows-amd64-installer.exe"
	if win.URL != wantURL {
		t.Fatalf("windows url = %q, want %q", win.URL, wantURL)
	}
	if win.Sig != wantURL+".minisig" {
		t.Fatalf("windows sig = %q, want %q.minisig", win.Sig, wantURL)
	}
	if win.SHA256 == "" || win.Size == 0 {
		t.Fatalf("windows asset missing digest/size: %+v", win)
	}
	// The Windows updater channel is the per-arch -installer.exe; the portable .zip
	// must not shadow the windows-arm64 key.
	arm, ok := m.Platforms["windows-arm64"]
	if !ok {
		t.Fatal("windows-arm64 missing")
	}
	if !strings.HasSuffix(arm.URL, "/Maddog-windows-arm64-installer.exe") {
		t.Fatalf("windows-arm64 url = %q, want the installer, not the portable zip", arm.URL)
	}
	// The updater-compatible tar must deterministically win over both guarded
	// human downloads, regardless of directory ordering.
	lin, ok := m.Platforms["linux-amd64"]
	if !ok {
		t.Fatal("linux-amd64 missing")
	}
	if !strings.HasSuffix(lin.URL, "/Maddog-linux-amd64-update.tar.gz") {
		t.Fatalf("linux-amd64 url = %q, want the updater-compatible tar", lin.URL)
	}
}

func TestMatchPlatformRejectsLinuxHumanDownloads(t *testing.T) {
	if got := matchPlatform("Maddog-linux-amd64.tar.gz"); got != "" {
		t.Fatalf("guarded human tar matched %q", got)
	}
	if got := matchPlatform("Maddog-linux-amd64.deb"); got != "" {
		t.Fatalf("deb matched %q", got)
	}
	if got := matchPlatform("Maddog-linux-amd64-update.tar.gz"); got != "linux-amd64" {
		t.Fatalf("updater tar matched %q", got)
	}
}
