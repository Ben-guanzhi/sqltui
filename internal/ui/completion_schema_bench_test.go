package ui

import (
	"fmt"
	"testing"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/query"
	"github.com/LinPr/sqltui/internal/reader"
)

// bigCatalogBackend is a live connection with many tables, each holding
// nCols columns, and it counts how often the completion schema assembly
// touches the catalog or the cached columns.
type bigCatalogBackend struct {
	nTables  int
	nCols    int
	nsCalls  int
	tblCalls int
}

func (b *bigCatalogBackend) Kind() string  { return "mysql" }
func (b *bigCatalogBackend) Title() string { return "bench" }
func (b *bigCatalogBackend) Run(string) (db.Result, error) {
	return db.Result{}, nil
}
func (b *bigCatalogBackend) Namespaces() ([]string, error) {
	b.nsCalls++
	return []string{"public"}, nil
}
func (b *bigCatalogBackend) Tables(string) ([]string, error) {
	b.tblCalls++
	ts := make([]string, b.nTables)
	for i := range ts {
		ts[i] = fmt.Sprintf("tbl_%03d", i)
	}
	return ts, nil
}
func (b *bigCatalogBackend) FetchTable(string, string, int) (*data.Frame, error) {
	return nil, fmt.Errorf("unused")
}
func (b *bigCatalogBackend) ColumnsMeta(_, table string) ([]db.ColumnMeta, error) {
	cols := make([]db.ColumnMeta, b.nCols)
	for i := range cols {
		cols[i].Name = fmt.Sprintf("%s_col_%02d", table, i)
	}
	return cols, nil
}
func (b *bigCatalogBackend) PrimaryKeys(string, string) ([]string, error) { return nil, nil }
func (b *bigCatalogBackend) ColumnIndexTypes(string, string) (map[string]string, error) {
	return nil, nil
}
func (b *bigCatalogBackend) Close() error { return nil }

// keystrokeBenchApp builds an App whose completion schema is the size of a
// real working session: a live catalog of big.nTables tables with columns
// warmed for all of them, plus a few engine-registered frames.
func keystrokeBenchApp(b *testing.B, big bigCatalogBackend) (*App, *schemaCache) {
	b.Helper()
	eng, err := query.NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { eng.Close() })
	a := New(Options{
		Engine:  eng,
		Backend: &big,
		Frames:  []reader.NamedFrame{{Name: "seed", Frame: scFrame()}},
	})
	{
		for t := 0; t < 20; t++ {
			cols := make([]data.Column, 15)
			for c := range cols {
				cols[c] = data.NewColumn(fmt.Sprintf("ecol_%d_%d", t, c), data.TypeString, []any{"x"})
			}
			if err := eng.Register(fmt.Sprintf("eng_%03d", t), &data.Frame{Columns: cols}); err != nil {
				b.Fatal(err)
			}
		}
	}
	a.WarmCompletionSchema()
	ts, err := big.Tables("public")
	if err != nil {
		b.Fatal(err)
	}
	a.WarmCompletionSchema(ts...)
	return a, a.completionCache()
}

// BenchmarkCompletionSchemaKeystroke simulates what qxEditor.refresh pays on
// every printable key in the SQL editor: rebuild the schema snapshot and run
// Suggest over it. Nothing about the catalog changes between keystrokes.
func BenchmarkCompletionSchemaKeystroke(b *testing.B) {
	a, _ := keystrokeBenchApp(b, bigCatalogBackend{nTables: 300, nCols: 25})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc := a.CompletionSchema()
		if len(sc.Tables) != 321 { // 300 live + 20 engine + "_"
			b.Fatalf("tables = %d", len(sc.Tables))
		}
		out := query.Suggest("select tbl_001_col_1", 19, sc)
		if len(out) == 0 {
			b.Fatal("no suggestions")
		}
	}
}
