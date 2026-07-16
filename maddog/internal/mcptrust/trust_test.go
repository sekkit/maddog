package mcptrust

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestIdentityFingerprintOmitsCredentialsAndIsStable(t *testing.T) {
	a := Identity{Type: "http", Name: "x", URL: "https://u:secret@example.test/mcp?token=one"}
	b := Identity{Type: "http", Name: "x", URL: "https://other:changed@example.test/mcp?token=two"}
	if IdentityFingerprint(a) != IdentityFingerprint(b) {
		t.Fatal("credential-bearing URL changed identity")
	}
}

func TestCapabilityFingerprintChangesOnSchemaOrClassification(t *testing.T) {
	a := Capability{Server: "s", Tool: "read", Schema: []byte(`{"type":"object"}`), ReadOnly: true}
	b := a
	b.ReadOnly = false
	if CapabilityFingerprint(a) == CapabilityFingerprint(b) {
		t.Fatal("classification change did not change fingerprint")
	}
}

func TestMemoryStoreRevocation(t *testing.T) {
	s := NewMemoryStore()
	r := Receipt{Identity: "i", Capability: "c", Approved: true}
	if err := s.Put(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background(), "i", "c"); err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(context.Background(), "i", "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background(), "i", "c"); err == nil {
		t.Fatal("revoked receipt returned")
	}
}

func TestFileStoreRejectsRevisionRollback(t *testing.T) {
	s := NewFileStore(t.TempDir() + "/receipts.json")
	if err := s.Put(context.Background(), Receipt{Identity: "i", Capability: "c", Approved: true, Revision: 4}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(context.Background(), Receipt{Identity: "i", Capability: "c", Approved: true, Revision: 3}); err == nil {
		t.Fatal("revision rollback accepted")
	}
	if _, err := s.Get(context.Background(), "i", "c"); err != nil {
		t.Fatal(err)
	}
}

func TestSameOriginRedirectRejectsCredentialLeak(t *testing.T) {
	origin, _ := url.Parse("https://example.test/mcp")
	other, _ := url.Parse("https://evil.test/mcp")
	if err := CheckRedirectOrigin(origin, other); err == nil {
		t.Fatal("cross-origin redirect allowed")
	}
	if err := CheckRedirectOrigin(origin, origin); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClientRejectsCrossOriginRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	c := CredentialSafeHTTPClient()
	_, err := c.Get(source.URL)
	if err == nil {
		t.Fatal("cross-origin redirect was followed")
	}
}
