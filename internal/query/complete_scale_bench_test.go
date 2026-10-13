package query

import (
	"fmt"
	"testing"

	"github.com/LinPr/sqltui/internal/data"
)

// benchCatalogEngine registers nTables tables of nCols columns each into a
// fresh engine, mimicking a file-mode session where several frames (plus the
// synced "_" table) are registered and the query editor re-derives the
// completion schema on every keystroke.
func benchCatalogEngine(b *testing.B, nTables, nCols int) *Engine {
	b.Helper()
	e, err := NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { e.Close() })
	for t := 0; t < nTables; t++ {
		cols := make([]data.Column, nCols)
		for c := 0; c < nCols; c++ {
			cols[c] = data.NewColumn(fmt.Sprintf("col_%d_%d", t, c), data.TypeString, []any{"x"})
		}
		if err := e.Register(fmt.Sprintf("table_%03d", t), &data.Frame{Columns: cols}); err != nil {
			b.Fatal(err)
		}
	}
	if err := e.Register("_", &data.Frame{Columns: []data.Column{
		data.NewColumn("id", data.TypeInt, []any{int64(1)}),
	}}); err != nil {
		b.Fatal(err)
	}
	return e
}

// BenchmarkEngineTableColumnsKeystrokes pins the per-keystroke price of the
// completion schema path: qxEditor.refresh calls App.CompletionSchema, which
// calls Engine.TableColumns once per keystroke. The schema does not change
// between keystrokes; every call after the first must be free.
func BenchmarkEngineTableColumnsKeystrokes(b *testing.B) {
	e := benchCatalogEngine(b, 60, 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := e.TableColumns()
		if err != nil || len(got) != 61 {
			b.Fatalf("TableColumns: %d tables, err %v", len(got), err)
		}
	}
}

// BenchmarkSuggestLargeCatalog measures the completer itself over a big
// frozen catalog (live tables with warmed columns): one call per keystroke.
func BenchmarkSuggestLargeCatalog(b *testing.B) {
	e := benchCatalogEngine(b, 60, 20)
	cols, err := e.TableColumns()
	if err != nil {
		b.Fatal(err)
	}
	sc := Schema{Tables: cols, Current: []string{"id", "col_1_1"}}
	input := "select col_1"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := Suggest(input, len(input), sc)
		if len(out) == 0 {
			b.Fatal("no suggestions")
		}
	}
}

// BenchmarkDotToken guards the token-position helpers against regressions
// while the schema plumbing is reworked.
func BenchmarkDotToken(b *testing.B) {
	cols := map[string][]string{}
	for t := 0; t < 300; t++ {
		c := make([]string, 25)
		for i := range c {
			c[i] = fmt.Sprintf("c%d_%d", t, i)
		}
		cols["tbl_"+fmt.Sprintf("%03d", t)] = c
	}
	sc := Schema{Tables: cols}
	input := "select * from tbl_042 join tbl_007 on tbl_007."
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := DotTable(input, len(input), sc)
		if !ok {
			b.Fatal("dot not resolved")
		}
	}
}
