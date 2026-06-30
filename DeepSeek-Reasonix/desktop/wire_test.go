package main

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestWireEventTabPreservesSharedRetryingFields(t *testing.T) {
	w := toWireTab(event.Event{Kind: event.Retrying, RetryAttempt: 3, RetryMax: 10}, "tab-1")
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if s := string(b); !strings.Contains(s, `"retryAttempt":3`) || !strings.Contains(s, `"retryMax":10`) {
		t.Errorf("retrying JSON = %s", s)
	}
}

func TestWireEventTabAddsRoutingAndSessionFields(t *testing.T) {
	w := toWireTab(event.Event{
		Kind:        event.Usage,
		SessionHit:  800,
		SessionMiss: 200,
	}, "tab-1")
	if w.Kind != "usage" || w.TabID != "tab-1" {
		t.Fatalf("tab wire = %+v", w)
	}
	if w.SessionHitTokens != 800 || w.SessionMissTokens != 200 {
		t.Fatalf("session tokens = hit:%d miss:%d", w.SessionHitTokens, w.SessionMissTokens)
	}
}

func TestWireEventTabPreservesMaddogRuntimeEvents(t *testing.T) {
	w := toWireTab(event.Event{
		Kind:  event.Advisor,
		Level: event.LevelWarn,
		Text:  "advisor consulted",
		Advisor: event.AdvisorConsultation{
			Advice:            "inspect the failing command",
			UsesThisSession:   2,
			MaxUsesPerSession: 10,
		},
	}, "tab-1")
	if w.Kind != "advisor" || w.Level != "warn" || w.Advisor == nil {
		t.Fatalf("advisor tab wire = %+v", w)
	}
	if w.Advisor.Advice != "inspect the failing command" || w.Advisor.MaxUsesPerSession != 10 {
		t.Fatalf("advisor payload = %+v", w.Advisor)
	}
}
