package plugin

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"maddog/internal/mcptrust"
	"maddog/internal/tool"
)

type strictTransport struct {
	calls, toolCalls atomic.Int32
	drift            bool
}

func (s *strictTransport) call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	s.calls.Add(1)
	if method == "tools/list" {
		schema := `{"type":"object"}`
		if s.drift {
			schema = `{"type":"object","required":["changed"]}`
		}
		return json.RawMessage(`{"tools":[{"name":"read","inputSchema":` + schema + `,"annotations":{"readOnlyHint":true}}]}`), nil
	}
	s.toolCalls.Add(1)
	return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
}

func TestStrictReaderRejectsLiveSchemaDriftBeforeTargetCall(t *testing.T) {
	tr := &strictTransport{drift: true}
	c := &Client{name: "s", t: tr, spec: Spec{StrictReader: true, TrustAuthority: allowAuthority{}, CatalogAuthority: allowAuthority{}}}
	rt := &remoteTool{client: c, rawName: "read", readOnly: true, capability: mcptrust.Capability{Server: "s", Tool: "read", Schema: canonicalizeSchema(json.RawMessage(`{"type":"object"}`)), ReadOnly: true}}
	if _, err := rt.Execute(context.Background(), nil); err != mcptrust.ErrCapabilityDrift {
		t.Fatalf("err=%v, want drift", err)
	}
	if got := tr.toolCalls.Load(); got != 0 {
		t.Fatalf("tools/call count=%d, want 0", got)
	}
}
func (*strictTransport) notify(context.Context, string, any) error { return nil }
func (*strictTransport) close()                                    {}

type denyAuthority struct{}

func (denyAuthority) Check(context.Context, mcptrust.Identity, mcptrust.Capability) error {
	return mcptrust.ErrRevoked
}

type allowAuthority struct{}

func (allowAuthority) Check(context.Context, mcptrust.Identity, mcptrust.Capability) error {
	return nil
}

func TestStrictReaderFailsClosedWithoutAuthority(t *testing.T) {
	tr := &strictTransport{}
	c := &Client{name: "s", t: tr, spec: Spec{StrictReader: true}}
	rt := &remoteTool{client: c, name: "mcp__s__read", rawName: "read", readOnly: true, capability: mcptrust.Capability{Server: "s", Tool: "read", Schema: canonicalizeSchema(json.RawMessage(`{"type":"object"}`)), ReadOnly: true}}
	if _, err := rt.Execute(context.Background(), nil); err == nil {
		t.Fatal("strict reader without authority executed")
	}
	if got := tr.calls.Load(); got != 0 {
		t.Fatalf("transport calls=%d, want 0", got)
	}
}

func TestStrictReaderRevalidatesAtDispatch(t *testing.T) {
	tr := &strictTransport{}
	c := &Client{name: "s", t: tr, spec: Spec{Type: "stdio", StrictReader: true, TrustAuthority: denyAuthority{}, CatalogAuthority: allowAuthority{}}}
	rt := &remoteTool{client: c, name: "mcp__s__read", rawName: "read", readOnly: true, capability: mcptrust.Capability{Server: "s", Tool: "read", Schema: canonicalizeSchema(json.RawMessage(`{"type":"object"}`)), ReadOnly: true}}
	if _, err := rt.Execute(context.Background(), nil); err != mcptrust.ErrRevoked {
		t.Fatalf("err=%v, want revoked", err)
	}
	if got := tr.calls.Load(); got != 0 {
		t.Fatalf("transport calls=%d, want zero after live revocation", got)
	}
}

func TestStrictRegistryWiresMCPDispatchAuthority(t *testing.T) {
	tr := &strictTransport{}
	c := &Client{name: "s", t: tr, spec: Spec{TrustAuthority: denyAuthority{}, CatalogAuthority: allowAuthority{}}}
	rt := &remoteTool{client: c, name: "mcp__s__read", rawName: "read", readOnly: true, readOnlyTrusted: true, capability: mcptrust.Capability{Server: "s", Tool: "read", Schema: canonicalizeSchema(json.RawMessage(`{"type":"object"}`)), ReadOnly: true}}
	src := tool.NewRegistry()
	src.Add(rt)
	strict, ok := tool.NewStrictReaderRegistry(src).Get(rt.Name())
	if !ok {
		t.Fatal("trusted MCP reader missing from strict registry")
	}
	if _, err := strict.Execute(context.Background(), nil); err != mcptrust.ErrRevoked {
		t.Fatalf("err=%v, want revoked", err)
	}
	if got := tr.calls.Load(); got != 0 {
		t.Fatalf("transport calls=%d, want 0", got)
	}
}
