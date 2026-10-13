package query

import (
	"reflect"
	"testing"

	"github.com/LinPr/sqltui/internal/data"
)

// colsFrame builds a one-row frame with n columns named col_0..col_{n-1}.
func colsFrame(prefix string, n int) *data.Frame {
	cols := make([]data.Column, n)
	for i := range cols {
		cols[i] = data.NewColumn(prefix+"col_"+string(rune('a'+i)), data.TypeString, []any{"v"})
	}
	return &data.Frame{Columns: cols}
}

// TestEngineTableColumnsMustHitCache pins the property the completion
// keystroke path depends on: repeated TableColumns calls over an unchanged
// schema answer entirely from the Register-time cache. Delete the cache and
// pragmaFalls grows with the call count, making this red (negative control).
func TestEngineTableColumnsMustHitCache(t *testing.T) {
	e := newTestEngine(t)
	for i := 0; i < 5; i++ {
		if err := e.Register("t"+string(rune('0'+i)), colsFrame("x", 3)); err != nil {
			t.Fatal(err)
		}
	}
	for call := 0; call < 4; call++ {
		got, err := e.TableColumns()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 5 {
			t.Fatalf("call %d: %d tables", call, len(got))
		}
	}
	e.mu.Lock()
	falls := e.pragmaFalls
	e.mu.Unlock()
	if falls != 0 {
		t.Errorf("PRAGMA fallbacks = %d, want 0 (cache must hit every call)", falls)
	}
}

// TestEngineTableColumnsCacheEqualsPragma is the behavioural-equivalence
// differential: the cached answer for every table must equal what the old
// path (PRAGMA table_info) would have returned.
func TestEngineTableColumnsCacheEqualsPragma(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Register("people", colsFrame("p", 4)); err != nil {
		t.Fatal(err)
	}
	if err := e.Register(`weird "name`, colsFrame("w", 2)); err != nil {
		t.Fatal(err)
	}
	got, err := e.TableColumns()
	if err != nil {
		t.Fatal(err)
	}
	for name, cols := range got {
		want, err := e.tableInfo(name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cols, want) {
			t.Errorf("cached %q = %v, PRAGMA = %v", name, cols, want)
		}
	}
}

// TestEngineTableColumnsCacheRevalidates pins the invalidation half of the
// mechanism: re-Registering a name replaces the cached columns and bumps the
// generation, Unregister removes both, and read-only calls never bump.
func TestEngineTableColumnsCacheRevalidates(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Register("t", colsFrame("a", 2)); err != nil {
		t.Fatal(err)
	}
	gen0 := e.SchemaGeneration()
	if _, err := e.TableColumns(); err != nil { // read must not bump
		t.Fatal(err)
	}
	if e.SchemaGeneration() != gen0 {
		t.Error("TableColumns bumped the generation")
	}

	if err := e.Register("t", colsFrame("b", 3)); err != nil {
		t.Fatal(err)
	}
	got, err := e.TableColumns()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bcol_a", "bcol_b", "bcol_c"}; !reflect.DeepEqual(got["t"], want) {
		t.Errorf("after replace: t = %v, want %v", got["t"], want)
	}
	if e.SchemaGeneration() == gen0 {
		t.Error("Register did not bump the generation")
	}

	if err := e.Unregister("t"); err != nil {
		t.Fatal(err)
	}
	got, err = e.TableColumns()
	if err != nil {
		t.Fatal(err)
	}
	if _, still := got["t"]; still {
		t.Error("unregistered table still cached")
	}
	if e.SchemaGeneration() == gen0 {
		t.Error("Unregister did not bump the generation")
	}
}
