package core

import (
	"context"
	"fmt"
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
	b := New(nil, Options{
		Kind: "postgresql",
		CurrentNS: func(context.Context) string {
			return "public"
		},
	})
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

// 表浏览翻页：单键 keyset 续读（游标类型化）+ 无单键 OFFSET 降级 + Total。
// TestBrowsePageCompositeKeyDegradesToOffset pins the composite-key
// degradation: a two-column PK must not install a single-column keyset (a
// cursor on pk0 alone skips rows sharing pk0 with a different pk1). The
// walk takes OFFSET over a full PK-ordered sort, reports no sort column
// (the UI takes its OFFSET branch), and pages through without skips.
func TestBrowsePageCompositeKeyDegradesToOffset(t *testing.T) {
	ctx := context.Background()
	drv, err := dbcoresqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	b := New(drv, Options{Kind: "sqlite", Title: "composite"})

	b.Run("CREATE TABLE ck(a INTEGER, b INTEGER, payload TEXT, PRIMARY KEY(a, b))")
	// 8 groups of 6 rows sharing the same `a`: a pk0-only keyset would jump
	// straight past five of every six rows.
	seed := "WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x < 47) "
	if _, err := b.Run(seed + "INSERT INTO ck(a,b,payload) SELECT x/6, x%6, 'row'||x FROM n"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	seen := map[string]bool{}
	pageSize := 20
	// The kernel's Page is 1-based: offset = (Page-1)*pageSize.
	req := &db.BrowsePageRequest{Limit: pageSize, Page: 1}
	for page := 1; page <= 5; page++ {
		res, err := b.BrowsePage(ctx, "main", "ck", req)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if res.SortColumn != "" {
			t.Fatalf("composite key reported sort column %q, want empty", res.SortColumn)
		}
		for r := 0; r < res.Frame.NumRows(); r++ {
			a := res.Frame.ColumnIndex("a")
			bb := res.Frame.ColumnIndex("b")
			av := fmt.Sprint(res.Frame.Row(r)[a])
			bv := fmt.Sprint(res.Frame.Row(r)[bb])
			id := av + ":" + bv
			if seen[id] {
				t.Fatalf("row %s seen twice (page %d)", id, page)
			}
			seen[id] = true
		}
		req = &db.BrowsePageRequest{Limit: pageSize, Page: page + 1}
	}
	if len(seen) != 48 {
		t.Fatalf("saw %d distinct rows, want 48 (no skips)", len(seen))
	}
}

func TestBrowsePage(t *testing.T) {
	ctx := context.Background()
	drv, err := dbcoresqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	b := New(drv, Options{Kind: "sqlite", Title: "browse"})

	b.Run("CREATE TABLE pk(id INTEGER PRIMARY KEY, name TEXT)")
	b.Run("CREATE TABLE pkless(name TEXT)")
	seed := "WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x < 300) "
	if _, err := b.Run(seed + "INSERT INTO pk(id,name) SELECT x, 'row'||x FROM n"); err != nil {
		t.Fatalf("seed pk table: %v", err)
	}

	// Page 1: the first window.
	res, err := b.BrowsePage(ctx, "main", "pk", &db.BrowsePageRequest{Limit: 200})
	frame, total, found := res.Frame, res.Total, res.TotalFound
	if res.SortColumn != "id" {
		t.Fatalf("sort column = %q, want id", res.SortColumn)
	}
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if frame.NumRows() != 200 || total != 300 || !found {
		t.Fatalf("page 1 = %d rows total=%d found=%v", frame.NumRows(), total, found)
	}
	// The cursor: the sort column (single PK) of the last row, as text.
	lastID, ok := frame.Cell(199, 0).(int64)
	if !ok || lastID != 200 {
		t.Fatalf("last pk = %#v, want 200", frame.Cell(199, 0))
	}

	// Page 2: keyset continuation from the cursor.
	res2, err := b.BrowsePage(ctx, "main", "pk", &db.BrowsePageRequest{After: []string{"200"}, Limit: 200})
	frame2 := res2.Frame
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if frame2.NumRows() != 100 {
		t.Fatalf("page 2 = %d rows, want 100", frame2.NumRows())
	}
	if id, _ := frame2.Cell(0, 0).(int64); id != 201 {
		t.Fatalf("page 2 first pk = %v, want 201", frame2.Cell(0, 0))
	}

	// A table without a single sort key degrades to OFFSET: Page=2 skips
	// the first window instead of keyset-walking.

	if _, err := b.Run(seed + "INSERT INTO pkless(name) SELECT 'row'||x FROM n"); err != nil {
		t.Fatal(err)
	}
	res3, err := b.BrowsePage(ctx, "main", "pkless", &db.BrowsePageRequest{Page: 2, Limit: 200})
	frame3 := res3.Frame
	if res3.SortColumn != "" {
		t.Fatalf("no single PK must mean OFFSET (sort column %q)", res3.SortColumn)
	}
	if err != nil {
		t.Fatalf("page 2 offset: %v", err)
	}
	if frame3.NumRows() != 100 {
		t.Fatalf("offset page 2 = %d rows, want 100", frame3.NumRows())
	}
}
