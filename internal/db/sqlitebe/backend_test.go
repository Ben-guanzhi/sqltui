package sqlitebe_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LinPr/sqltui/internal/db/sqlitebe"
)

func newTestBackend(t *testing.T) *sqlitebe.Backend {
	t.Helper()

	file := filepath.Join(t.TempDir(), "adapter.db")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}

	be, err := sqlitebe.Connect(sqlitebe.Config{FilePath: file})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { be.Close() })
	return be
}

func TestConnectRejectsMissingFile(t *testing.T) {
	if _, err := sqlitebe.Connect(sqlitebe.Config{FilePath: filepath.Join(t.TempDir(), "nope.db")}); err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestConnectRejectsEmptyPath(t *testing.T) {
	if _, err := sqlitebe.Connect(sqlitebe.Config{FilePath: "  "}); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestBackendEndToEnd(t *testing.T) {
	be := newTestBackend(t)

	if got := be.Kind(); got != "sqlite" {
		t.Fatalf("Kind = %q, want sqlite", got)
	}
	if !strings.HasPrefix(be.Title(), "sqlite://") || !strings.HasSuffix(be.Title(), "adapter.db") {
		t.Fatalf("unexpected Title: %q", be.Title())
	}

	// exec: create + insert
	res, err := be.Run(`CREATE TABLE pets (id INTEGER PRIMARY KEY, name TEXT, sound TEXT)`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Exec == nil || res.Frame != nil {
		t.Fatalf("CREATE should produce an exec result, got %+v", res)
	}

	res, err = be.Run(`INSERT INTO pets (name, sound) VALUES ('cat', 'meow'), ('dog', 'woof'), ('fish', NULL)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if res.Exec == nil {
		t.Fatal("INSERT should produce an exec result")
	}
	if res.Exec.RowsAffected != 3 {
		t.Fatalf("RowsAffected = %d, want 3", res.Exec.RowsAffected)
	}
	if !res.Exec.HasLastInsert {
		t.Fatal("sqlite exec results should report HasLastInsert")
	}
	if res.Exec.LastInsertID != 3 {
		t.Fatalf("LastInsertID = %d, want 3", res.Exec.LastInsertID)
	}

	// query
	res, err = be.Run(`SELECT id, name, sound FROM pets ORDER BY id`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if res.Frame == nil || res.Exec != nil {
		t.Fatalf("SELECT should produce a frame, got %+v", res)
	}
	f := res.Frame
	if f.NumRows() != 3 || f.NumCols() != 3 {
		t.Fatalf("frame shape %dx%d, want 3x3", f.NumRows(), f.NumCols())
	}
	if got := f.ColumnNames(); got[0] != "id" || got[1] != "name" || got[2] != "sound" {
		t.Fatalf("unexpected columns: %v", got)
	}
	if f.Cell(0, 1) != "cat" || f.Cell(1, 2) != "woof" {
		t.Fatalf("unexpected cells: %v / %v", f.Cell(0, 1), f.Cell(1, 2))
	}
	// kernel values keep NULL distinct from the empty string (the pre-kernel
	// raw-byte scan collapsed both)
	if f.Cell(2, 2) != nil {
		t.Fatalf("NULL cell = %#v, want nil", f.Cell(2, 2))
	}

	// INSERT ... RETURNING routes to the query path
	res, err = be.Run(`INSERT INTO pets (name, sound) VALUES ('owl', NULL) RETURNING id, name`)
	if err != nil {
		t.Fatalf("insert returning: %v", err)
	}
	if res.Frame == nil || res.Frame.NumRows() != 1 {
		t.Fatalf("INSERT RETURNING should produce a one-row frame, got %+v", res)
	}

	// namespaces / tables
	ns, err := be.Namespaces()
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	// SQLite reports the attached databases; the main one is always present.
	found := false
	for _, n := range ns {
		if n == "main" || n == "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Namespaces = %v, want the main database", ns)
	}
	tables, err := be.Tables("ignored")
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	if len(tables) != 1 || tables[0] != "pets" {
		t.Fatalf("Tables = %v, want [pets]", tables)
	}

	// primary keys and column metadata via the kernel catalog
	pks, err := be.PrimaryKeys("", "pets")
	if err != nil {
		t.Fatalf("PrimaryKeys: %v", err)
	}
	if len(pks) != 1 || pks[0] != "id" {
		t.Fatalf("PrimaryKeys = %v, want [id]", pks)
	}
	cols, err := be.ColumnsMeta("", "pets")
	if err != nil {
		t.Fatalf("ColumnsMeta: %v", err)
	}
	if len(cols) != 3 || cols[0].Name != "id" || cols[1].Name != "name" || cols[2].Name != "sound" {
		t.Fatalf("ColumnsMeta = %+v", cols)
	}
	// SQLite's pragma reports notnull=0 even for an implicit rowid-alias
	// INTEGER PRIMARY KEY, so id reads as nullable — same as the pre-kernel
	// DAO, which mapped the same pragma column.
	if cols[0].IsNullable != "YES" || cols[1].IsNullable != "YES" || cols[2].IsNullable != "YES" {
		t.Fatalf("ColumnsMeta = %+v", cols)
	}

	// index labels via the kernel's structured index metadata
	if _, err := be.Run(`CREATE UNIQUE INDEX pets_name ON pets (name)`); err != nil {
		t.Fatalf("create index: %v", err)
	}
	idx, err := be.ColumnIndexTypes("", "pets")
	if err != nil {
		t.Fatalf("ColumnIndexTypes: %v", err)
	}
	if idx["id"] != "PK" || idx["name"] != "UNIQUE" {
		t.Fatalf("ColumnIndexTypes = %v, want id=PK name=UNIQUE", idx)
	}

	// fetch table with limit
	frame, err := be.FetchTable("", "pets", 2)
	if err != nil {
		t.Fatalf("FetchTable: %v", err)
	}
	if frame.NumRows() != 2 || frame.NumCols() != 3 {
		t.Fatalf("FetchTable shape %dx%d, want 2x3", frame.NumRows(), frame.NumCols())
	}
}

func TestFetchTableQuotesIdentifier(t *testing.T) {
	be := newTestBackend(t)

	if _, err := be.Run(`CREATE TABLE "odd ""name" (v TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := be.Run(`INSERT INTO "odd ""name" VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	frame, err := be.FetchTable("", `odd "name`, 10)
	if err != nil {
		t.Fatalf("FetchTable: %v", err)
	}
	if frame.NumRows() != 1 || frame.Cell(0, 0) != "x" {
		t.Fatalf("unexpected frame: rows=%d cell=%v", frame.NumRows(), frame.Cell(0, 0))
	}
}

func TestRunEmptyStatement(t *testing.T) {
	be := newTestBackend(t)

	if _, err := be.Run("   "); err == nil {
		t.Fatal("expected error for empty statement")
	}
}

func TestTableTarget(t *testing.T) {
	if got := sqlitebe.Target("anything", "orders"); got != `"orders"` {
		t.Errorf("target = %s, want \"orders\" (namespace ignored)", got)
	}
}

func TestPrimaryKeysOrdinalAndColumnsMetaDefaults(t *testing.T) {
	be := newTestBackend(t)

	if _, err := be.Run(`CREATE TABLE kv (k TEXT, v TEXT, PRIMARY KEY (k, v))`); err != nil {
		t.Fatalf("create kv: %v", err)
	}
	pks, err := be.PrimaryKeys("", "kv")
	if err != nil {
		t.Fatalf("PrimaryKeys(kv): %v", err)
	}
	if len(pks) != 2 || pks[0] != "k" || pks[1] != "v" {
		t.Fatalf("expected [k v] in pk-ordinal order, got %v", pks)
	}

	if _, err := be.Run(`CREATE TABLE meta_demo (
		id INTEGER NOT NULL PRIMARY KEY,
		email TEXT NOT NULL DEFAULT 'unknown',
		note TEXT
	)`); err != nil {
		t.Fatalf("create meta_demo: %v", err)
	}
	cols, err := be.ColumnsMeta("", "meta_demo")
	if err != nil {
		t.Fatalf("ColumnsMeta(meta_demo): %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d (%v)", len(cols), cols)
	}
	if cols[0].Name != "id" || cols[0].IsNullable != "NO" || cols[0].Default != "" {
		t.Fatalf("id column = %+v", cols[0])
	}
	if cols[1].Name != "email" || cols[1].IsNullable != "NO" || cols[1].Default != "'unknown'" {
		t.Fatalf("email column = %+v", cols[1])
	}
	if cols[2].Name != "note" || cols[2].IsNullable != "YES" || cols[2].Default != "" {
		t.Fatalf("note column = %+v", cols[2])
	}
}
