package ui

import (
	"reflect"
	"testing"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/query"
	"github.com/LinPr/sqltui/internal/reader"
)

func snapMapPtr(m map[string][]string) uintptr {
	if m == nil {
		return 0
	}
	return reflect.ValueOf(m).Pointer()
}

// TestCompletionSchemaSnapshotMustHit pins the property the SQL editor pays
// for on every keystroke: while the catalog, connection, engine schema and
// viewed frame are unchanged, CompletionSchema rebuilds exactly once and
// hands back the same shared snapshot. Without the memo the builds counter
// grows per call, making this red (negative control).
func TestCompletionSchemaSnapshotMustHit(t *testing.T) {
	eng, err := query.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	f := scFrame()
	if err := eng.Register("people", f); err != nil {
		t.Fatal(err)
	}
	be := &scCountingBackend{}
	a := New(Options{Engine: eng, Backend: be, Frames: []reader.NamedFrame{{Name: "p", Frame: f}}})
	a.WarmCompletionSchema()
	a.WarmCompletionSchema("users")

	c := a.completionCache()
	sc1 := a.CompletionSchema()
	sc2 := a.CompletionSchema()
	sc3 := a.CompletionSchema()

	c.mu.Lock()
	builds := c.builds
	c.mu.Unlock()
	if builds != 1 {
		t.Errorf("builds = %d after three unchanged calls, want 1", builds)
	}
	if snapMapPtr(sc1.Tables) != snapMapPtr(sc2.Tables) || snapMapPtr(sc2.Tables) != snapMapPtr(sc3.Tables) {
		t.Error("snapshot not shared across unchanged calls")
	}
	if !reflect.DeepEqual(sc2.Tables["users"], []string{"id", "label"}) {
		t.Errorf("users = %v", sc2.Tables["users"])
	}
}

// TestCompletionSchemaSnapshotInvalidates is the other half: every mutation
// that changes the answer must force exactly one rebuild, never serve stale.
func TestCompletionSchemaSnapshotInvalidates(t *testing.T) {
	eng, err := query.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	f := scFrame()
	be := &scCountingBackend{}
	a := New(Options{Engine: eng, Backend: be, Frames: []reader.NamedFrame{{Name: "p", Frame: f}}})
	a.WarmCompletionSchema()

	c := a.completionCache()
	mark := func() int { c.mu.Lock(); defer c.mu.Unlock(); return c.builds }
	a.CompletionSchema()
	before := mark()

	// 1. A newly warmed table's columns must appear.
	a.WarmCompletionSchema("orders")
	if got := a.CompletionSchema().Tables["orders"]; len(got) != 2 {
		t.Fatalf("orders columns after warm = %v", got)
	}
	mid := mark()
	if mid != before+1 {
		t.Fatalf("warm did not rebuild exactly once: %d -> %d", before, mid)
	}
	before = mid

	// 2. A frame switch changes Current and the synced "_" table.
	f2 := &data.Frame{Columns: []data.Column{
		data.NewColumn("alpha", data.TypeString, []any{"x"}),
	}}
	a.Update(ApplyFrameMsg{Frame: f2, Crumb: "next"})
	sc := a.CompletionSchema()
	if !reflect.DeepEqual(sc.Current, []string{"alpha"}) {
		t.Errorf("Current after frame switch = %v", sc.Current)
	}
	mid = mark()
	if mid != before+1 {
		t.Fatalf("frame switch did not rebuild exactly once: %d -> %d", before, mid)
	}
	before = mid

	// 3. An engine Register (the registerTable path) changes the schema.
	if err := eng.Register("extra", f2); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.CompletionSchema().Tables["extra"]; !ok {
		t.Error("engine register not visible in schema")
	}
	mid = mark()
	if mid != before+1 {
		t.Fatalf("engine register did not rebuild exactly once: %d -> %d", before, mid)
	}
	before = mid

	// 4. A connection swap resets and rebuilds.
	be2 := &scCountingBackend{}
	a.Update(SetBackendMsg{Backend: be2})
	if tables := a.CompletionSchema().Tables; len(tables) == 0 {
		t.Error("engine tables vanished after swap")
	}
	mid = mark()
	if mid != before+1 {
		t.Fatalf("backend swap did not rebuild exactly once: %d -> %d", before, mid)
	}
}
