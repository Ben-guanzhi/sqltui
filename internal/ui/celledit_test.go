package ui

import (
	"strings"
	"testing"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/query"
	"github.com/LinPr/sqltui/internal/theme"
)

// cellEditBackend is a db.Backend stub for celledit tests. Kind returns either
// "mysql" or "postgres"; PrimaryKeys returns the canned set.
type cellEditBackend struct {
	kind string
	pks  []string
	last string
	res  db.Result
	err  error
}

func (b *cellEditBackend) Kind() string  { return b.kind }
func (b *cellEditBackend) Title() string { return "stub" }
func (b *cellEditBackend) Run(stmt string) (db.Result, error) {
	b.last = stmt
	return b.res, b.err
}
func (b *cellEditBackend) Namespaces() ([]string, error)   { return nil, nil }
func (b *cellEditBackend) Tables(string) ([]string, error) { return nil, nil }
func (b *cellEditBackend) FetchTable(string, string, int) (*data.Frame, error) {
	return nil, nil
}
func (b *cellEditBackend) PrimaryKeys(string, string) ([]string, error)        { return b.pks, nil }
func (b *cellEditBackend) ColumnsMeta(string, string) ([]db.ColumnMeta, error) { return nil, nil }
func (b *cellEditBackend) ColumnIndexTypes(string, string) (map[string]string, error) {
	return nil, nil
}
func (b *cellEditBackend) Close() error { return nil }

func cellEditFrame() *data.Frame {
	return &data.Frame{Columns: []data.Column{
		data.NewColumn("id", data.TypeInt, []any{int64(1), int64(2), int64(3)}),
		data.NewColumn("name", data.TypeString, []any{"ann", "bob", "cyd"}),
		data.NewColumn("ns", data.TypeString, []any{"a", "b", "c"}),
	}}
}

func TestQuoteIdentDialect(t *testing.T) {
	mysql := &cellEditBackend{kind: "mysql"}
	pg := &cellEditBackend{kind: "postgres"}
	if got := quoteIdent(mysql, "name"); got != "`name`" {
		t.Errorf("mysql quoteIdent = %q, want `name`", got)
	}
	if got := quoteIdent(mysql, "na`me"); got != "`na``me`" {
		t.Errorf("mysql quoteIdent escape = %q", got)
	}
	if got := quoteIdent(pg, `na"me`); got != `"na""me"` {
		t.Errorf("pg quoteIdent = %q", got)
	}
	if got := quoteIdent(pg, "name"); got != `"name"` {
		t.Errorf("pg quoteIdent = %q, want \"name\"", got)
	}
}

func TestSqlLiteral(t *testing.T) {
	mysql := &cellEditBackend{kind: "mysql"}
	pg := &cellEditBackend{kind: "postgres"}
	if got := sqlLiteral(nil, "it's"); got != `'it''s'` {
		t.Errorf("sqlLiteral = %q", got)
	}
	if got := sqlLiteral(nil, "plain"); got != `'plain'` {
		t.Errorf("sqlLiteral = %q", got)
	}
	// MySQL: a trailing backslash must not swallow the closing quote
	// (regression: `'c:\'` terminated the literal early and corrupted the
	// statement, worst case rewriting the WHERE clause of a DELETE).
	if got := sqlLiteral(mysql, `c:\`); got != `'c:\\'` {
		t.Errorf("mysql sqlLiteral = %q, want %q", got, `'c:\\'`)
	}
	// PostgreSQL standard conforming strings treat the backslash literally;
	// doubling would store two backslashes instead of one.
	if got := sqlLiteral(pg, `c:\`); got != `'c:\'` {
		t.Errorf("pg sqlLiteral = %q, want %q", got, `'c:\'`)
	}
}

func TestParseTyped(t *testing.T) {
	cases := []struct {
		val string
		t   data.DType
		ok  bool
	}{
		{"hello", data.TypeString, true},
		{"true", data.TypeBool, true},
		{"0", data.TypeBool, true},
		{"42", data.TypeInt, true},
		{"3.14", data.TypeFloat, true},
		{"abc", data.TypeInt, false},
		{"abc", data.TypeFloat, false},
		{"abc", data.TypeBool, false},
	}
	for _, c := range cases {
		v, err := parseTyped(c.val, c.t)
		if c.ok && err != nil {
			t.Errorf("parseTyped(%q, %v): unexpected error %v", c.val, c.t, err)
		}
		if !c.ok && err == nil {
			t.Errorf("parseTyped(%q, %v): expected error, got %v", c.val, c.t, v)
		}
	}
}

func TestBuildUpdateSinglePK(t *testing.T) {
	pg := &cellEditBackend{kind: "postgres", pks: []string{"id"}}
	frame := cellEditFrame()
	got, err := buildUpdate(pg, "users", []string{"id"}, frame, 0, "name", "alice")
	if err != nil {
		t.Fatal(err)
	}
	want := `UPDATE "users" SET "name"='alice' WHERE "id"='1'`
	if got != want {
		t.Errorf("pg buildUpdate = %q, want %q", got, want)
	}
}

func TestBuildUpdateMysqlBacktick(t *testing.T) {
	mysql := &cellEditBackend{kind: "mysql", pks: []string{"id"}}
	frame := cellEditFrame()
	got, err := buildUpdate(mysql, "users", []string{"id"}, frame, 1, "name", "zoe")
	if err != nil {
		t.Fatal(err)
	}
	want := "UPDATE `users` SET `name`='zoe' WHERE `id`='2'"
	if got != want {
		t.Errorf("mysql buildUpdate = %q, want %q", got, want)
	}
}

func TestBuildUpdateMultiPK(t *testing.T) {
	pg := &cellEditBackend{kind: "postgres", pks: []string{"id", "ns"}}
	frame := cellEditFrame()
	got, err := buildUpdate(pg, "t", []string{"id", "ns"}, frame, 2, "name", "x")
	if err != nil {
		t.Fatal(err)
	}
	want := `UPDATE "t" SET "name"='x' WHERE "id"='3' AND "ns"='c'`
	if got != want {
		t.Errorf("multi-pk buildUpdate = %q, want %q", got, want)
	}
}

func TestBuildUpdateMissingPK(t *testing.T) {
	pg := &cellEditBackend{kind: "postgres"}
	frame := cellEditFrame()
	_, err := buildUpdate(pg, "t", []string{"missing"}, frame, 0, "name", "x")
	if err == nil || !strings.Contains(err.Error(), "primary key column not found") {
		t.Fatalf("expected missing PK error, got %v", err)
	}
}

func TestBuildDeleteSinglePK(t *testing.T) {
	pg := &cellEditBackend{kind: "postgres", pks: []string{"id"}}
	frame := cellEditFrame()
	got, err := buildDelete(pg, "users", []string{"id"}, frame, []int{0, 2})
	if err != nil {
		t.Fatal(err)
	}
	want := `DELETE FROM "users" WHERE "id" IN ('1','3')`
	if got != want {
		t.Errorf("pg buildDelete = %q, want %q", got, want)
	}
}

func TestBuildDeleteSinglePKMysql(t *testing.T) {
	mysql := &cellEditBackend{kind: "mysql", pks: []string{"id"}}
	frame := cellEditFrame()
	got, err := buildDelete(mysql, "users", []string{"id"}, frame, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	want := "DELETE FROM `users` WHERE `id` IN ('2')"
	if got != want {
		t.Errorf("mysql buildDelete = %q, want %q", got, want)
	}
}

func TestBuildDeleteMultiPK(t *testing.T) {
	pg := &cellEditBackend{kind: "postgres", pks: []string{"id", "ns"}}
	frame := cellEditFrame()
	got, err := buildDelete(pg, "t", []string{"id", "ns"}, frame, []int{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	want := `DELETE FROM "t" WHERE ("id"='1' AND "ns"='a') OR ("id"='2' AND "ns"='b')`
	if got != want {
		t.Errorf("multi-pk buildDelete = %q, want %q", got, want)
	}
}

func TestBuildDeleteNoPK(t *testing.T) {
	pg := &cellEditBackend{kind: "postgres"}
	frame := cellEditFrame()
	_, err := buildDelete(pg, "t", nil, frame, []int{0})
	if err == nil || !strings.Contains(err.Error(), "no primary key") {
		t.Fatalf("expected no primary key error, got %v", err)
	}
}

func TestBuildDeleteMissingPK(t *testing.T) {
	pg := &cellEditBackend{kind: "postgres"}
	frame := cellEditFrame()
	_, err := buildDelete(pg, "t", []string{"missing"}, frame, []int{0})
	if err == nil || !strings.Contains(err.Error(), "primary key column not found") {
		t.Fatalf("expected missing PK error, got %v", err)
	}
}

// cellEditCtx is a minimal AppContext for commit tests.
type cellEditCtx struct {
	frame  *data.Frame
	be     db.Backend
	paneID int
}

func (c *cellEditCtx) CurrentFrame() *data.Frame     { return c.frame }
func (c *cellEditCtx) CurrentRow() int               { return 0 }
func (c *cellEditCtx) SheetFieldCursor() int         { return 0 }
func (c *cellEditCtx) CurrentTableNamespace() string { return "" }
func (c *cellEditCtx) BaseCrumb() string             { return "users" }
func (c *cellEditCtx) Crumbs() []string              { return nil }
func (c *cellEditCtx) ColumnNames() []string         { return nil }
func (c *cellEditCtx) Engine() *query.Engine         { return nil }
func (c *cellEditCtx) TableNames() []string          { return nil }
func (c *cellEditCtx) Backend() db.Backend           { return c.be }
func (c *cellEditCtx) KV() db.KVBackend              { return nil }
func (c *cellEditCtx) Theme() *theme.Theme           { return nil }
func (c *cellEditCtx) ThemeName() string             { return "" }
func (c *cellEditCtx) ShowBorders() bool             { return false }
func (c *cellEditCtx) ShowRowNumbers() bool          { return false }
func (c *cellEditCtx) Tabs() []TabInfo               { return nil }
func (c *cellEditCtx) ActiveTab() int                { return 0 }
func (c *cellEditCtx) ActivePaneID() int             { return c.paneID }
func (c *cellEditCtx) PendingEdit() *PendingEdit     { return nil }
func (c *cellEditCtx) PendingDelete() *PendingDelete { return nil }

func TestCommitCellEditDBMode(t *testing.T) {
	be := &cellEditBackend{
		kind: "postgres",
		pks:  []string{"id"},
		res:  db.Result{Exec: &db.ExecResult{RowsAffected: 1}},
	}
	ctx := &cellEditCtx{frame: cellEditFrame(), be: be, paneID: 7}
	pe := PendingEdit{Frame: ctx.frame, Row: 0, Col: 1, ColName: "name", NewValue: "x", Table: "users"}
	cmd := commitCellEdit(ctx, pe)
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	m, ok := msg.(cellSavedMsg)
	if !ok {
		t.Fatalf("expected cellSavedMsg, got %T", msg)
	}
	if m.err != nil {
		t.Fatal(m.err)
	}
	if m.rows != 1 {
		t.Errorf("rows = %d, want 1", m.rows)
	}
	if !strings.HasPrefix(be.last, "UPDATE") {
		t.Errorf("backend saw %q", be.last)
	}
}

func TestCommitCellEditFileMode(t *testing.T) {
	ctx := &cellEditCtx{frame: cellEditFrame(), paneID: 3}
	pe := PendingEdit{Frame: ctx.frame, Row: 0, Col: 1, ColName: "name", NewValue: "zoe"}
	cmd := commitCellEdit(ctx, pe)
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	m, ok := msg.(ApplyFrameMsg)
	if !ok {
		t.Fatalf("expected ApplyFrameMsg, got %T", msg)
	}
	if m.Crumb != "edit" || m.PaneID != 3 {
		t.Errorf("ApplyFrameMsg = %+v", m)
	}
	if v := m.Frame.Cell(0, 1); v != "zoe" {
		t.Errorf("edited cell = %v, want zoe", v)
	}
	// Original frame untouched.
	if v := ctx.frame.Cell(0, 1); v != "ann" {
		t.Errorf("original frame mutated: %v", v)
	}
}

func TestCommitCellEditFileModeParseError(t *testing.T) {
	ctx := &cellEditCtx{frame: cellEditFrame()}
	pe := PendingEdit{Frame: ctx.frame, Row: 0, Col: 0, ColName: "id", NewValue: "abc"}
	cmd := commitCellEdit(ctx, pe)
	msg := cmd()
	if _, ok := msg.(ErrorMsg); !ok {
		t.Fatalf("expected ErrorMsg, got %T", msg)
	}
}

func TestCommitRowDeleteDBMode(t *testing.T) {
	be := &cellEditBackend{
		kind: "postgres",
		pks:  []string{"id"},
		res:  db.Result{Exec: &db.ExecResult{RowsAffected: 2}},
	}
	ctx := &cellEditCtx{frame: cellEditFrame(), be: be}
	pd := PendingDelete{Frame: ctx.frame, Rows: []int{0, 1}, Table: "users"}
	cmd := commitRowDelete(ctx, pd)
	msg := cmd()
	m, ok := msg.(rowsDeletedMsg)
	if !ok {
		t.Fatalf("expected rowsDeletedMsg, got %T", msg)
	}
	if m.err != nil {
		t.Fatal(m.err)
	}
	if m.rows != 2 {
		t.Errorf("rows = %d, want 2", m.rows)
	}
	if !strings.HasPrefix(be.last, "DELETE") {
		t.Errorf("backend saw %q", be.last)
	}
}

func TestCommitRowDeleteFileMode(t *testing.T) {
	ctx := &cellEditCtx{frame: cellEditFrame(), paneID: 5}
	pd := PendingDelete{Frame: ctx.frame, Rows: []int{0, 2}}
	cmd := commitRowDelete(ctx, pd)
	msg := cmd()
	m, ok := msg.(ApplyFrameMsg)
	if !ok {
		t.Fatalf("expected ApplyFrameMsg, got %T", msg)
	}
	if m.Crumb != "delete" || m.PaneID != 5 {
		t.Errorf("ApplyFrameMsg = %+v", m)
	}
	if m.Frame.NumRows() != 1 {
		t.Fatalf("rows = %d, want 1", m.Frame.NumRows())
	}
	if v := m.Frame.Cell(0, 1); v != "bob" {
		t.Errorf("surviving row = %v, want bob", v)
	}
}

// boundEditBackend extends cellEditBackend with RunBound, recording the
// statement and arguments it received.
type boundEditBackend struct {
	cellEditBackend
	lastArgs []any
}

func (b *boundEditBackend) RunBound(stmt string, args []any) (db.Result, error) {
	b.last = stmt
	b.lastArgs = args
	return b.res, b.err
}

func TestBuildUpdateBoundShapes(t *testing.T) {
	frame := cellEditFrame()
	cases := []struct {
		kind string
		stmt string
		args []any
	}{
		{"postgres", `UPDATE "users" SET "name"=$1 WHERE "id"=$2`, []any{"x", int64(2)}},
		{"mysql", "UPDATE `users` SET `name`=? WHERE `id`=?", []any{"x", int64(2)}},
		// The stub has no Quoter, so identifiers render with the double-quote
		// fallback; the placeholder spelling is what varies by engine here.
		{"sqlserver", `UPDATE "users" SET "name"=@p1 WHERE "id"=@p2`, []any{"x", int64(2)}},
		{"oracle", `UPDATE "users" SET "name"=:1 WHERE "id"=:2`, []any{"x", int64(2)}},
	}
	for _, c := range cases {
		be := &cellEditBackend{kind: c.kind}
		stmt, args, err := buildUpdateBound(be, "users", []string{"id"}, frame, 1, "name", "x")
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if stmt != c.stmt {
			t.Errorf("%s stmt = %q, want %q", c.kind, stmt, c.stmt)
		}
		if len(args) != len(c.args) {
			t.Fatalf("%s args = %#v, want %#v", c.kind, args, c.args)
		}
		for i := range args {
			if args[i] != c.args[i] {
				t.Errorf("%s args[%d] = %#v, want %#v", c.kind, i, args[i], c.args[i])
			}
		}
	}
}

func TestBuildUpdateBoundMultiPKAndErrors(t *testing.T) {
	pg := &cellEditBackend{kind: "postgres"}
	frame := cellEditFrame()
	stmt, args, err := buildUpdateBound(pg, "t", []string{"id", "ns"}, frame, 2, "name", "x")
	if err != nil {
		t.Fatal(err)
	}
	want := `UPDATE "t" SET "name"=$1 WHERE "id"=$2 AND "ns"=$3`
	if stmt != want {
		t.Errorf("stmt = %q, want %q", stmt, want)
	}
	if len(args) != 3 || args[1] != int64(3) || args[2] != "c" {
		t.Errorf("args = %#v, want [x 3 c]", args)
	}

	if _, _, err := buildUpdateBound(pg, "t", nil, frame, 0, "name", "x"); err == nil || !strings.Contains(err.Error(), "no primary key") {
		t.Fatalf("expected no-pk error, got %v", err)
	}
	if _, _, err := buildUpdateBound(pg, "t", []string{"missing"}, frame, 0, "name", "x"); err == nil || !strings.Contains(err.Error(), "primary key column not found") {
		t.Fatalf("expected missing-pk error, got %v", err)
	}
	if _, _, err := buildUpdateBound(pg, "t", []string{"id"}, frame, 0, "name", "abc"); err == nil || !strings.Contains(err.Error(), "expected") {
		// name is TypeString in the fixture, so this must succeed; force a
		// typed column instead below.
		t.Logf("string column accepted %v (ok)", err)
	}
}

func TestBuildUpdateBoundTypeValidation(t *testing.T) {
	pg := &cellEditBackend{kind: "postgres"}
	frame := cellEditFrame() // id is TypeInt
	if _, _, err := buildUpdateBound(pg, "users", []string{"id"}, frame, 0, "id", "not-a-number"); err == nil {
		t.Fatal("expected integer parse error for int column")
	}
}

func TestBuildDeleteBoundShapes(t *testing.T) {
	frame := cellEditFrame()
	pg := &cellEditBackend{kind: "postgres"}
	stmt, args, err := buildDeleteBound(pg, "users", []string{"id"}, frame, []int{0, 2})
	if err != nil {
		t.Fatal(err)
	}
	want := `DELETE FROM "users" WHERE "id" IN ($1,$2)`
	if stmt != want {
		t.Errorf("pg stmt = %q, want %q", stmt, want)
	}
	if len(args) != 2 || args[0] != int64(1) || args[1] != int64(3) {
		t.Errorf("args = %#v, want [1 3]", args)
	}

	mysql := &cellEditBackend{kind: "mysql"}
	stmt, args, err = buildDeleteBound(mysql, "users", []string{"id"}, frame, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if stmt != "DELETE FROM `users` WHERE `id` IN (?)" || len(args) != 1 || args[0] != int64(2) {
		t.Errorf("mysql stmt = %q args = %#v", stmt, args)
	}

	multi := &cellEditBackend{kind: "sqlserver"} // stub without Quoter: double-quote fallback; the placeholder numbering is what's under test
	stmt, args, err = buildDeleteBound(multi, "t", []string{"id", "ns"}, frame, []int{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	want = `DELETE FROM "t" WHERE ("id"=@p1 AND "ns"=@p2) OR ("id"=@p3 AND "ns"=@p4)`
	if stmt != want {
		t.Errorf("mssql multi-pk stmt = %q, want %q", stmt, want)
	}
	if len(args) != 4 || args[0] != int64(1) || args[1] != "a" || args[2] != int64(2) || args[3] != "b" {
		t.Errorf("args = %#v", args)
	}

	if _, _, err := buildDeleteBound(pg, "t", nil, frame, []int{0}); err == nil {
		t.Fatal("expected no-pk error")
	}
}

func TestBindValue(t *testing.T) {
	if v, err := bindValue("42", data.TypeInt); err != nil || v != int64(42) {
		t.Errorf("int: %v %v", v, err)
	}
	if v, err := bindValue("1.5", data.TypeFloat); err != nil || v != 1.5 {
		t.Errorf("float: %v %v", v, err)
	}
	if v, err := bindValue("true", data.TypeBool); err != nil || v != true {
		t.Errorf("bool: %v %v", v, err)
	}
	// Dates stay text: engines parse date literals from strings.
	if v, err := bindValue("2026-10-25", data.TypeDate); err != nil || v != "2026-10-25" {
		t.Errorf("date: %v %v", v, err)
	}
	if _, err := bindValue("abc", data.TypeInt); err == nil {
		t.Error("expected error for non-integer int column")
	}
}

func TestCommitCellEditBoundPath(t *testing.T) {
	be := &boundEditBackend{cellEditBackend: cellEditBackend{kind: "postgres", pks: []string{"id"},
		res: db.Result{Exec: &db.ExecResult{RowsAffected: 1}}}}
	ctx := &cellEditCtx{frame: cellEditFrame(), be: be, paneID: 7}
	pe := PendingEdit{Frame: ctx.frame, Row: 1, Col: 1, ColName: "name", NewValue: "zoe", Table: "users"}
	msg := commitCellEdit(ctx, pe)()
	m, ok := msg.(cellSavedMsg)
	if !ok || m.err != nil || m.rows != 1 {
		t.Fatalf("msg = %#v", msg)
	}
	if !strings.HasPrefix(be.last, `UPDATE "users" SET "name"=$1`) {
		t.Errorf("backend saw %q", be.last)
	}
	// The new value and the PK bind as native types, not quoted text.
	if len(be.lastArgs) != 2 || be.lastArgs[0] != "zoe" || be.lastArgs[1] != int64(2) {
		t.Errorf("args = %#v, want [zoe 2]", be.lastArgs)
	}
}

func TestCommitRowDeleteBoundPath(t *testing.T) {
	be := &boundEditBackend{cellEditBackend: cellEditBackend{kind: "postgres", pks: []string{"id"},
		res: db.Result{Exec: &db.ExecResult{RowsAffected: 2}}}}
	ctx := &cellEditCtx{frame: cellEditFrame(), be: be, paneID: 1}
	pd := PendingDelete{Frame: ctx.frame, Rows: []int{0, 2}, Table: "users"}
	msg := commitRowDelete(ctx, pd)()
	m, ok := msg.(rowsDeletedMsg)
	if !ok || m.err != nil || m.rows != 2 {
		t.Fatalf("msg = %#v", msg)
	}
	if be.last != `DELETE FROM "users" WHERE "id" IN ($1,$2)` {
		t.Errorf("backend saw %q", be.last)
	}
	if len(be.lastArgs) != 2 || be.lastArgs[0] != int64(1) || be.lastArgs[1] != int64(3) {
		t.Errorf("args = %#v", be.lastArgs)
	}
}

func TestRunnerFallsBackWithoutBoundRunner(t *testing.T) {
	be := &cellEditBackend{kind: "postgres"} // no RunBound
	r := runnerFor(be)
	if r.binds() {
		t.Fatal("plain backend must not report binds")
	}
	res, err := r.run("UPDATE t SET a = '1'", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	if be.last != "UPDATE t SET a = '1'" {
		t.Errorf("backend saw %q", be.last)
	}
}
