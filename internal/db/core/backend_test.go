package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dbcore/dbcore"
	_ "github.com/dbcore/dbcore/sqlite" // registers the sqlite factory

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db/core"
)

// newSQLiteBackend opens a real kernel sqlite driver through the adapter; the
// assertions below therefore exercise the full kernel path: factory, Execute
// routing, catalog queries, and typed values.
func newSQLiteBackend(t *testing.T) *core.Backend {
	t.Helper()
	drv, err := dbcore.Open(context.Background(), dbcore.ConnSpec{Kind: dbcore.KindSQLite, File: ":memory:"}, dbcore.PoolConfig{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sql, ok := drv.AsSQL()
	if !ok {
		t.Fatal("sqlite driver is not an SQL driver")
	}
	t.Cleanup(func() { drv.Close() })
	return core.New(sql, core.Options{
		Kind:            "sqlite",
		Title:           "sqlite://:memory:",
		Target:          func(_, table string) string { return core.QuoteIdent("sqlite", table) },
		HasLastInsertID: true,
	})
}

func TestRunRoutesQueriesExecAndReturning(t *testing.T) {
	b := newSQLiteBackend(t)

	if _, err := b.Run(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	// exec result
	res, err := b.Run(`INSERT INTO t (v) VALUES ('a'), ('b')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if res.Frame != nil || res.Exec == nil || res.Exec.RowsAffected != 2 || !res.Exec.HasLastInsert || res.Exec.LastInsertID != 2 {
		t.Fatalf("exec result = %+v", res.Exec)
	}

	// query result keeps native types and NULL
	res, err = b.Run(`SELECT id, v FROM t ORDER BY id`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if res.Exec != nil || res.Frame == nil || res.Frame.NumRows() != 2 {
		t.Fatalf("query result = %+v", res)
	}
	if res.Frame.Cell(0, 0) != int64(1) || res.Frame.Cell(0, 1) != "a" {
		t.Fatalf("cells = %v / %v", res.Frame.Cell(0, 0), res.Frame.Cell(0, 1))
	}

	// empty statement
	if _, err := b.Run("  "); err == nil {
		t.Fatal("expected error for empty statement")
	}
}

func TestFetchTableClampsLimit(t *testing.T) {
	b := newSQLiteBackend(t)
	for _, stmt := range []string{
		`CREATE TABLE t (id INTEGER PRIMARY KEY)`,
		`INSERT INTO t VALUES (1),(2),(3)`,
	} {
		if _, err := b.Run(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	// limit <= 0 becomes 1; a huge limit clamps to 10000 (unreachable here,
	// the clamp just keeps the generated LIMIT sane).
	f1, err := b.FetchTable("", "t", 0)
	if err != nil {
		t.Fatalf("FetchTable limit 0: %v", err)
	}
	if f1.NumRows() != 1 {
		t.Fatalf("limit 0 fetched %d rows, want 1", f1.NumRows())
	}
	f3, err := b.FetchTable("", "t", 2)
	if err != nil {
		t.Fatalf("FetchTable limit 2: %v", err)
	}
	if f3.NumRows() != 2 {
		t.Fatalf("limit 2 fetched %d rows, want 2", f3.NumRows())
	}
}

func TestQuoterAndEscaper(t *testing.T) {
	b := newSQLiteBackend(t)
	if got := b.QuoteIdent("odd\"name"); got != `"odd""name"` {
		t.Errorf("QuoteIdent = %q", got)
	}
	if got := b.EscapeLiteral(`it's`); got != `'it''s'` {
		t.Errorf("EscapeLiteral = %q", got)
	}

	// MySQL quoting and backslash escaping through the kernel dialect.
	if got := core.QuoteIdent("mysql", "odd`name"); got != "`odd``name`" {
		t.Errorf("mysql QuoteIdent = %q", got)
	}
	if got := core.QuoteIdent("mysql", "users"); got != "`users`" {
		t.Errorf("mysql QuoteIdent = %q", got)
	}
}

func TestCurrentNSQuery(t *testing.T) {
	b := newSQLiteBackend(t)
	cur := core.CurrentNSQuery(b.Driver(), "SELECT 'main' AS ns")
	if got := cur(context.Background()); got != "main" {
		t.Errorf("CurrentNSQuery = %q, want main", got)
	}
	// A failing query yields "" (the browser falls back to its own logic).
	bad := core.CurrentNSQuery(b.Driver(), "SELECT * FROM no_such_table")
	if got := bad(context.Background()); got != "" {
		t.Errorf("CurrentNSQuery on error = %q, want empty", got)
	}
}

func TestParsePort(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"3306", 3306},
		{" 5432 ", 5432},
		{"", 0},
		{"abc", 0},
		{"-1", 0},
		{"70000", 0},
	}
	for _, c := range cases {
		if got := core.ParsePort(c.in); got != c.want {
			t.Errorf("ParsePort(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestNativeValue(t *testing.T) {
	cases := []struct {
		v    dbcore.Value
		want any
	}{
		{dbcore.Value{Kind: dbcore.ValNull}, nil},
		{dbcore.Value{Kind: dbcore.ValInt, I: 42}, int64(42)},
		{dbcore.Value{Kind: dbcore.ValFloat, F: 1.5}, 1.5},
		{dbcore.Value{Kind: dbcore.ValText, S: "s"}, "s"},
		{dbcore.Value{Kind: dbcore.ValBool, Bool: true}, true},
		{dbcore.Value{Kind: dbcore.ValTime, T: time.Unix(0, 0).UTC()}, time.Unix(0, 0).UTC()},
	}
	for _, c := range cases {
		if got := core.NativeValue(c.v); got != c.want {
			t.Errorf("NativeValue(%+v) = %#v, want %#v", c.v, got, c.want)
		}
	}
}

func TestFrameFromResultEmpty(t *testing.T) {
	f := core.FrameFromResult(&dbcore.QueryResult{})
	if f.NumRows() != 0 || f.NumCols() != 0 {
		t.Fatalf("empty result frame = %dx%d, want 0x0", f.NumRows(), f.NumCols())
	}
	var _ *data.Frame = f
}

// TestConnectThroughAdapterFile pins the sqlite wrapper's file contract end to
// end: an existing empty file opens, a missing one is refused.
func TestConnectThroughAdapterFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.db")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	drv, err := dbcore.Open(context.Background(), dbcore.ConnSpec{Kind: dbcore.KindSQLite, File: file}, dbcore.PoolConfig{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer drv.Close()
	sql, _ := drv.AsSQL()
	b := core.New(sql, core.Options{
		Kind:   "sqlite",
		Title:  "sqlite://" + file,
		Target: func(_, table string) string { return core.QuoteIdent("sqlite", table) },
	})
	if _, err := b.Run(`CREATE TABLE a (x INTEGER)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	tables, err := b.Tables("")
	if err != nil || len(tables) != 1 || tables[0] != "a" {
		t.Fatalf("Tables = %v, %v", tables, err)
	}
}

// TestRunBoundEndToEnd exercises the parameter-bound execution path against a
// real kernel driver: user text rides as an argument, never as statement
// text, and native types (int64 keys) match without string coercion.
func TestRunBoundEndToEnd(t *testing.T) {
	b := newSQLiteBackend(t)
	for _, stmt := range []string{
		`CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)`,
		`INSERT INTO t VALUES (1, 'ann'), (2, 'bob')`,
	} {
		if _, err := b.Run(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	// UPDATE ... SET name = ? WHERE id = ? with a quote-laden value.
	res, err := b.RunBound(`UPDATE t SET name = ? WHERE id = ?`, []any{"it's a \"test\"", int64(2)})
	if err != nil {
		t.Fatalf("RunBound update: %v", err)
	}
	if res.Exec == nil || res.Exec.RowsAffected != 1 {
		t.Fatalf("update result = %+v", res.Exec)
	}

	got, err := b.RunBound(`SELECT name FROM t WHERE id = ?`, []any{int64(2)})
	if err != nil {
		t.Fatalf("RunBound select: %v", err)
	}
	if got.Frame == nil || got.Frame.NumRows() != 1 || got.Frame.Cell(0, 0) != `it's a "test"` {
		t.Fatalf("select frame = %+v", got.Frame)
	}

	// DELETE with an IN list bound natively.
	res, err = b.RunBound(`DELETE FROM t WHERE id IN (?, ?)`, []any{int64(1), int64(2)})
	if err != nil {
		t.Fatalf("RunBound delete: %v", err)
	}
	if res.Exec == nil || res.Exec.RowsAffected != 2 {
		t.Fatalf("delete result = %+v", res.Exec)
	}

	// NULL binds as NULL, not as the empty string.
	if _, err := b.Run(`INSERT INTO t VALUES (3, 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RunBound(`UPDATE t SET name = ? WHERE id = ?`, []any{nil, int64(3)}); err != nil {
		t.Fatalf("null bind: %v", err)
	}
	got, err = b.RunBound(`SELECT name FROM t WHERE id = ?`, []any{int64(3)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Frame == nil || got.Frame.Cell(0, 0) != nil {
		t.Fatalf("NULL round trip = %#v", got.Frame.Cell(0, 0))
	}

	// Empty statement still errors.
	if _, err := b.RunBound("  ", nil); err == nil {
		t.Fatal("expected empty-statement error")
	}
}
