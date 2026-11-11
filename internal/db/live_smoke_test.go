// Live smoke tests for the kernel-backed engine wrappers, gated per engine on
// a CSV environment variable so they skip silently without a real server
// (same convention as dbcore's conn_live_test.go):
//
//	SQLTUI_SMOKE_PG="host,port,user,pass,db"
//	SQLTUI_SMOKE_MYSQL="host,port,user,pass,db"
//	SQLTUI_SMOKE_MSSQL="host,port,user,pass,db"
//	SQLTUI_SMOKE_ORACLE="host,port,user,pass,service"
//	SQLTUI_SMOKE_REDIS="host,port"
//
// Each SQL test walks the exact path the TUI drives: wrapper Connect →
// catalog browse (Namespaces / Tables / PrimaryKeys / ColumnsMeta /
// ColumnIndexTypes / FetchTable) → parameter-bound UPDATE and DELETE via
// db.BoundRunner (identifiers quoted through the backend's Quoter, markers
// spelled by sqltext.Placeholder — the same helpers ui.celledit uses) →
// read-back verification.
package db_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dbcore/dbcore/sqltext"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/mysqlbe"
	"github.com/LinPr/sqltui/internal/db/oraclebe"
	"github.com/LinPr/sqltui/internal/db/postgresbe"
	"github.com/LinPr/sqltui/internal/db/redisbe"
	"github.com/LinPr/sqltui/internal/db/sqlserverbe"
)

// smokeEnv reads a CSV gate variable; nil means "skip this engine".
func smokeEnv(t *testing.T, name string, want int) []string {
	t.Helper()
	raw, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) != want {
		t.Fatalf("%s must be %d comma-separated fields, got %q", name, want, raw)
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// smokeQuoteIdent mirrors ui.quoteIdent: probe the backend's Quoter, fall
// back to double quotes.
func smokeQuoteIdent(be db.Backend, name string) string {
	if q, ok := be.(db.Quoter); ok {
		return q.QuoteIdent(name)
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// smokePh spells the idx-th bind marker for the backend's engine.
func smokePh(be db.Backend, idx int) string {
	return sqltext.Placeholder(be.Kind(), idx)
}

// colIdx finds a frame column case-insensitively (Oracle folds identifiers
// to uppercase; the other engines keep the created spelling).
func colIdx(frame *data.Frame, name string) int {
	for i, n := range frame.ColumnNames() {
		if strings.EqualFold(n, name) {
			return i
		}
	}
	return -1
}

// runSQLSmoke walks connect → browse → bound edit → read back on one wrapper.
// ns is the namespace argument the browser would pass (mysql/mssql: database,
// pg: schema, oracle: login user); createStmts run before browsing, and
// boundStmts below build from the table name as the catalog returns it.
func runSQLSmoke(t *testing.T, be db.Backend, ns, createStmt string, insertStmts []string) {
	t.Helper()

	// Pre-clean a leftover table from an earlier run (unquoted name folds to
	// the engine's case rules everywhere).
	_, _ = be.Run("DROP TABLE sqltui_smoke")

	// create + insert
	res, err := be.Run(createStmt)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Exec == nil {
		t.Fatalf("CREATE routed as query: %+v", res)
	}
	for _, ins := range insertStmts {
		if _, err := be.Run(ins); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// browse: namespaces (log only — oracle's login user may be filtered from
	// the listing; the browser handles a missing current namespace), tables,
	// and the per-table catalog.
	nss, err := be.Namespaces()
	if err != nil {
		t.Fatalf("Namespaces: %v", err)
	}
	if len(nss) == 0 {
		t.Fatal("Namespaces returned nothing")
	}
	t.Logf("namespaces(%d): %v", len(nss), nss)

	tables, err := be.Tables(ns)
	if err != nil {
		t.Fatalf("Tables(%q): %v", ns, err)
	}
	var tbl string
	for _, name := range tables {
		if strings.EqualFold(name, "sqltui_smoke") {
			tbl = name // use the catalog's spelling (Oracle: SQLTUI_SMOKE)
			break
		}
	}
	if tbl == "" {
		t.Fatalf("sqltui_smoke not in Tables(%q): %v", ns, tables)
	}

	pks, err := be.PrimaryKeys(ns, tbl)
	if err != nil {
		t.Fatalf("PrimaryKeys: %v", err)
	}
	if len(pks) != 1 || !strings.EqualFold(pks[0], "id") {
		t.Fatalf("PrimaryKeys = %v, want [id]", pks)
	}

	cols, err := be.ColumnsMeta(ns, tbl)
	if err != nil {
		t.Fatalf("ColumnsMeta: %v", err)
	}
	if len(cols) != 2 || !strings.EqualFold(cols[0].Name, "id") || !strings.EqualFold(cols[1].Name, "name") {
		t.Fatalf("ColumnsMeta = %+v", cols)
	}
	// Canonical column spellings come from the catalog — Oracle folds
	// unquoted identifiers to uppercase, and the real UI builds its
	// statements from exactly these names.
	colID, colName := cols[0].Name, cols[1].Name

	idx, err := be.ColumnIndexTypes(ns, tbl)
	if err != nil {
		t.Fatalf("ColumnIndexTypes: %v", err)
	}
	pkLabel := ""
	for c, label := range idx {
		if strings.EqualFold(c, "id") {
			pkLabel = label
		}
	}
	if pkLabel != "PK" {
		t.Fatalf("ColumnIndexTypes = %v, want id=PK", idx)
	}

	frame, err := be.FetchTable(ns, tbl, 10)
	if err != nil {
		t.Fatalf("FetchTable: %v", err)
	}
	if frame.NumRows() != 2 || frame.NumCols() != 2 {
		t.Fatalf("FetchTable shape %dx%d, want 2x2", frame.NumRows(), frame.NumCols())
	}
	idCol, nameCol := colIdx(frame, "id"), colIdx(frame, "name")
	if idCol < 0 || nameCol < 0 {
		t.Fatalf("frame columns = %v", frame.ColumnNames())
	}

	// edit: parameter-bound UPDATE with a quote-laden unicode value —
	// nothing here may reach the statement text.
	br, ok := be.(db.BoundRunner)
	if !ok {
		t.Fatalf("%s backend does not implement db.BoundRunner", be.Kind())
	}
	nasty := "已编辑-'quote\"and\\slash-" + be.Kind()
	update := "UPDATE " + smokeQuoteIdent(be, tbl) +
		" SET " + smokeQuoteIdent(be, colName) + "=" + smokePh(be, 1) +
		" WHERE " + smokeQuoteIdent(be, colID) + "=" + smokePh(be, 2)
	res, err = br.RunBound(update, []any{nasty, int64(1)})
	if err != nil {
		t.Fatalf("RunBound update: %v", err)
	}
	if res.Exec == nil || res.Exec.RowsAffected != 1 {
		t.Fatalf("update result = %+v", res.Exec)
	}

	// bound DELETE of the other row
	del := "DELETE FROM " + smokeQuoteIdent(be, tbl) +
		" WHERE " + smokeQuoteIdent(be, colID) + "=" + smokePh(be, 1)
	res, err = br.RunBound(del, []any{int64(2)})
	if err != nil {
		t.Fatalf("RunBound delete: %v", err)
	}
	if res.Exec == nil || res.Exec.RowsAffected != 1 {
		t.Fatalf("delete result = %+v", res.Exec)
	}

	// read back: exactly one row, carrying the nasty value verbatim
	frame2, err := be.FetchTable(ns, tbl, 10)
	if err != nil {
		t.Fatalf("FetchTable after edit: %v", err)
	}
	if frame2.NumRows() != 1 {
		t.Fatalf("rows after delete = %d, want 1", frame2.NumRows())
	}
	if got := frame2.CellString(0, nameCol); got != nasty {
		t.Fatalf("edited value = %q, want %q", got, nasty)
	}
	if got := frame2.CellString(0, idCol); got != "1" {
		t.Fatalf("surviving id = %q, want 1", got)
	}

	// cleanup
	if _, err := be.Run("DROP TABLE " + smokeQuoteIdent(be, tbl)); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := be.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestLiveSmokePostgres(t *testing.T) {
	env := smokeEnv(t, "SQLTUI_SMOKE_PG", 5)
	if env == nil {
		t.Skip("SQLTUI_SMOKE_PG not set")
	}
	be, err := postgresbe.Connect(context.Background(), postgresbe.Config{
		UserName: env[2], Password: env[3], Host: env[0], Port: env[1], DbName: env[4], SslMode: "disable",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	runSQLSmoke(t, be, "public",
		"CREATE TABLE sqltui_smoke (id INTEGER PRIMARY KEY, name TEXT)",
		[]string{"INSERT INTO sqltui_smoke (id, name) VALUES (1, 'first'), (2, 'second')"})
}

func TestLiveSmokeMySQL(t *testing.T) {
	env := smokeEnv(t, "SQLTUI_SMOKE_MYSQL", 5)
	if env == nil {
		t.Skip("SQLTUI_SMOKE_MYSQL not set")
	}
	be, err := mysqlbe.Connect(context.Background(), mysqlbe.Config{
		UserName: env[2], Password: env[3], Host: env[0], Port: env[1], DbName: env[4],
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	runSQLSmoke(t, be, env[4],
		"CREATE TABLE sqltui_smoke (id INT PRIMARY KEY, name VARCHAR(200))",
		[]string{"INSERT INTO sqltui_smoke (id, name) VALUES (1, 'first'), (2, 'second')"})
}

func TestLiveSmokeSQLServer(t *testing.T) {
	env := smokeEnv(t, "SQLTUI_SMOKE_MSSQL", 5)
	if env == nil {
		t.Skip("SQLTUI_SMOKE_MSSQL not set")
	}
	be, err := sqlserverbe.Connect(context.Background(), sqlserverbe.Config{
		UserName: env[2], Password: env[3], Host: env[0], Port: env[1], DbName: env[4],
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	runSQLSmoke(t, be, env[4],
		"CREATE TABLE sqltui_smoke (id INT PRIMARY KEY, name NVARCHAR(200))",
		[]string{"INSERT INTO sqltui_smoke (id, name) VALUES (1, 'first'), (2, 'second')"})
}

func TestLiveSmokeOracle(t *testing.T) {
	env := smokeEnv(t, "SQLTUI_SMOKE_ORACLE", 5)
	if env == nil {
		t.Skip("SQLTUI_SMOKE_ORACLE not set")
	}
	be, err := oraclebe.Connect(context.Background(), oraclebe.Config{
		UserName: env[2], Password: env[3], Host: env[0], Port: env[1], DbName: env[4],
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Unquoted identifiers fold to uppercase; inserts go one per statement
	// (Oracle has no multi-row VALUES). The namespace is the login user as
	// the catalog spells it: uppercase (all_users returns uppercase names,
	// and the browser passes those back).
	runSQLSmoke(t, be, strings.ToUpper(env[2]),
		"CREATE TABLE sqltui_smoke (id INTEGER PRIMARY KEY, name VARCHAR2(200))",
		[]string{
			"INSERT INTO sqltui_smoke (id, name) VALUES (1, 'first')",
			"INSERT INTO sqltui_smoke (id, name) VALUES (2, 'second')",
		})
}

func TestLiveSmokeRedis(t *testing.T) {
	env := smokeEnv(t, "SQLTUI_SMOKE_REDIS", 2)
	if env == nil {
		t.Skip("SQLTUI_SMOKE_REDIS not set")
	}
	be, err := redisbe.Connect(context.Background(), redisbe.Config{Host: env[0], Port: env[1], RdbNum: "0"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer be.Close()

	const nasty = "你好-'\"\\world"
	if _, err := be.Do([]string{"SET", "sqltui:smoke", nasty}); err != nil {
		t.Fatalf("SET: %v", err)
	}
	// Value renders as indented JSON; decode it and compare exactly.
	val, err := be.Value("sqltui:smoke")
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var decoded string
	if err := json.Unmarshal([]byte(val), &decoded); err != nil {
		t.Fatalf("Value is not JSON: %v (%q)", err, val)
	}
	if decoded != nasty {
		t.Fatalf("decoded value = %q, want %q", decoded, nasty)
	}
	keys, err := be.ScanKeys("string")
	if err != nil {
		t.Fatalf("ScanKeys: %v", err)
	}
	found := false
	for _, k := range keys {
		if k == "sqltui:smoke" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sqltui:smoke not in ScanKeys: %v", keys)
	}
	if _, err := be.Do([]string{"DEL", "sqltui:smoke"}); err != nil {
		t.Fatalf("DEL: %v", err)
	}
}
