package mcpcatalog

import (
	"crypto/ed25519"
	"testing"
)

func TestSignedCatalogVerifiesCanonicalPayload(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	c := Catalog{Version: 1, Entries: []Entry{{Name: "x", Type: "stdio", Command: "x", Args: []string{"--pinned"}}}}
	s := Sign(c, priv)
	if err := Verify(s, pub); err != nil {
		t.Fatal(err)
	}
	s.Payload.Entries[0].Args[0] = "--changed"
	if err := Verify(s, pub); err == nil {
		t.Fatal("modified catalog verified")
	}
}

func TestProductionPublicKeyIsUsable(t *testing.T) {
	if got := ProductionPublicKey(); len(got) != ed25519.PublicKeySize {
		t.Fatalf("ProductionPublicKey length=%d, want %d", len(got), ed25519.PublicKeySize)
	}
}

func TestExactPinRequiresEveryArgument(t *testing.T) {
	p := Pin{Command: "x", Args: []string{"--a", "1"}}
	if !p.Matches("x", []string{"--a", "1"}) {
		t.Fatal("exact pin rejected")
	}
	if p.Matches("x", []string{"--a", "1", "--extra"}) {
		t.Fatal("extra argument accepted")
	}
}

func TestFileStoreRejectsCatalogRollback(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	s := NewFileStore(t.TempDir() + "/catalog.json")
	if err := s.Save(Sign(Catalog{Version: 2}, priv)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Sign(Catalog{Version: 1}, priv)); err == nil {
		t.Fatal("catalog rollback accepted")
	}
	if _, err := s.Load(pub); err != nil {
		t.Fatal(err)
	}
}
