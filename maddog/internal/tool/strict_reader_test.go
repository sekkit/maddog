package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type readerTestTool struct {
	name                string
	readOnly, untrusted bool
}

func (t *readerTestTool) Name() string                                           { return t.name }
func (*readerTestTool) Description() string                                      { return "" }
func (*readerTestTool) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (*readerTestTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (t *readerTestTool) ReadOnly() bool                                         { return t.readOnly }
func (t *readerTestTool) PlanModeUntrustedReadOnly() bool                        { return t.untrusted }

type readerProxy struct {
	*readerTestTool
	target Tool
}

func (p *readerProxy) TargetTool() Tool { return p.target }

func TestStrictReaderRegistryChecksResolvedProxyTarget(t *testing.T) {
	src := NewRegistry()
	src.Add(&readerTestTool{name: "read", readOnly: true})
	src.Add(&readerTestTool{name: "hint", readOnly: true, untrusted: true})
	src.Add(&readerProxy{readerTestTool: &readerTestTool{name: "proxy", readOnly: true}, target: &readerTestTool{name: "write", readOnly: false}})
	got := NewStrictReaderRegistry(src).Names()
	if len(got) != 1 || got[0] != "read" {
		t.Fatalf("strict names=%v, want [read]", got)
	}
}
