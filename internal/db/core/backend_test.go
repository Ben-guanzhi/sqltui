package core

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/LinPr/sqltui/internal/db"
)

// CurrentNamespace memoizes: the first resolution is the only query. The app
// warms it the moment a connection lands, so the update loop's lookups
// (delete confirmation, completion warm) never block on a cold 30s round
// trip — on postgres a cold lookup is a real query.
func TestCurrentNamespaceMemoizes(t *testing.T) {
	var calls atomic.Int64
	b := New(nil, Options{
		Kind: "postgresql",
		CurrentNS: func(context.Context) string {
			return "public"
		},
	})
	_ = calls

	if _, ok := db.Backend(b).(db.NamespaceWarmer); !ok {
		t.Fatal("Backend must implement db.NamespaceWarmer")
	}

	// Cold read resolves and memoizes.
	if got := b.CurrentNamespace(); got != "public" {
		t.Fatalf("cold CurrentNamespace = %q", got)
	}
	b.WarmNamespace() // memo hit: no extra work
	if got := b.CurrentNamespace(); got != "public" {
		t.Fatalf("warm CurrentNamespace = %q", got)
	}

	// A backend that was warmed BEFORE its first read never queries from the
	// read path at all: the warm is the only resolution.
	var resolved atomic.Int64
	b2 := New(nil, Options{
		Kind: "postgresql",
		CurrentNS: func(context.Context) string {
			resolved.Add(1)
			return "app"
		},
	})
	b2.WarmNamespace()
	if resolved.Load() != 1 {
		t.Fatalf("warm resolved %d times, want 1", resolved.Load())
	}
	for range 3 {
		if got := b2.CurrentNamespace(); got != "app" {
			t.Fatalf("CurrentNamespace = %q", got)
		}
	}
	if resolved.Load() != 1 {
		t.Fatalf("read path re-resolved: %d times", resolved.Load())
	}

	// Backends without a resolver stay at the empty default.
	b3 := New(nil, Options{Kind: "mysql"})
	if got := b3.CurrentNamespace(); got != "" {
		t.Fatalf("no-resolver CurrentNamespace = %q, want empty", got)
	}
	b3.WarmNamespace() // no-op, must not panic
}
