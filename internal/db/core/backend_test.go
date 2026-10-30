package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LinPr/sqltui/internal/db"
	dbcoresqlite "github.com/dbcore/dbcore/sqlite"
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

// ctrl+c 的取消必须能停一条在跑的长语句：注册表把 cancel handle 递给内核
// 驱动（sqlite 走 sqlite3_interrupt），CancelRunning 后语句以错误收场而非
// 跑满 2 亿次递归。第二次取消必须如实报告没有在飞语句。
func TestCancelRunningStopsStatement(t *testing.T) {
	drv, err := dbcoresqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	b := New(drv, Options{Kind: "sqlite", Title: "test"})
	if _, err := b.Run("CREATE TABLE t(x INTEGER)"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := b.RunBound(
			"WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x < 200000000) SELECT count(*) FROM n",
			nil,
		)
		done <- err
	}()

	time.Sleep(150 * time.Millisecond) // let the statement register and start
	if !b.CancelRunning() {
		t.Fatal("CancelRunning found nothing in flight")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the cancelled statement must end with an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not stop the statement")
	}
	if b.CancelRunning() {
		t.Fatal("a second cancel must find nothing in flight")
	}
}
