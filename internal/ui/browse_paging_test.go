package ui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/reader"
)

// fakeBrowseBackend is a minimal db.Backend + db.BrowseBackend for browse
// paging tests: every page returns a fresh window of rows and reports a
// sort column so the walk advances by keyset cursor.
type fakeBrowseBackend struct {
	fakeBackend
	pageCalls int
}

func (f *fakeBrowseBackend) BrowsePage(_ context.Context, _, _ string, req *db.BrowsePageRequest) (*db.BrowsePageResult, error) {
	f.pageCalls++
	// Two pages of 2 rows each; columns match testFrame (id, name, comment)
	// so AppendRow into the base frame doesn't panic on width mismatch.
	// The sort column is "id" (single PK keyset).
	frame := data.New("id", "name", "comment")
	start := req.Page // page 2 starts at id 3
	for i := 0; i < 2; i++ {
		frame.AppendRow([]any{start + i, "row", "c"})
	}
	return &db.BrowsePageResult{
		Frame:      frame,
		Total:      4,
		TotalFound: true,
		SortColumn: "id",
	}, nil
}

// TestBrowsePagingWalkAndRefresh pins the full browse-paging UI chain:
// opening a table tab seeds the walk, ">" fetches the next window, the
// result appends to the base frame and advances the cursor, and a refresh
// (ReplaceBaseMsg) re-seeds the walk back to page 1.
func TestBrowsePagingWalkAndRefresh(t *testing.T) {
	bb := &fakeBrowseBackend{}
	a := New(Options{
		Frames:  []reader.NamedFrame{{Name: "t", Frame: testFrame(2)}},
		Backend: bb,
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Open a table tab: ApplyFrameMsg with source records the table and
	// seedBrowse creates the initial walk state.
	a.Update(ApplyFrameMsg{
		Frame:           testFrame(2),
		NewTab:          true,
		TabTitle:        "orders",
		SourceNamespace: "sales",
		SourceTable:     "orders",
	})

	st := a.activeBrowse()
	if st == nil {
		t.Fatal("browse state not seeded after table open")
	}
	if st.page != 1 || st.done {
		t.Fatalf("initial walk = page %d done %v, want page 1 not done", st.page, st.done)
	}
	if got := a.pane().Base().NumRows(); got != 2 {
		t.Fatalf("base frame rows = %d, want 2", got)
	}

	// ">" dispatches ActNextPage: the returned cmd fetches page 2.
	_, cmd := a.Update(key(">"))
	if cmd == nil {
		t.Fatal("ActNextPage returned no command")
	}
	msg := cmd()
	pm, ok := msg.(BrowsePageMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want BrowsePageMsg", msg)
	}
	if pm.err != nil {
		t.Fatalf("BrowsePage error: %v", pm.err)
	}

	// Applying the message appends the window to the base frame and
	// advances the cursor to the last row's sort value.
	_, applyCmd := a.Update(pm)
	_ = applyCmd // browsePageApplied returns a toast cmd, optional

	if got := a.pane().Base().NumRows(); got != 4 {
		t.Fatalf("base frame rows after page 2 = %d, want 4", got)
	}
	st = a.activeBrowse()
	if st.after == nil || len(st.after) != 1 {
		t.Fatalf("cursor after = %v, want single keyset value", st.after)
	}
	if st.page != 2 {
		t.Fatalf("page after keyset advance = %d, want 2 (the counter feeds the toast)", st.page)
	}
	if st.loaded != 4 {
		t.Fatalf("loaded = %d, want 4", st.loaded)
	}

	// A refresh (ReplaceBaseMsg) swaps the base frame and re-seeds the
	// walk: the fresh window is page 1 again, cursor cleared, done false.
	a.Update(ReplaceBaseMsg{Frame: testFrame(2), PaneID: a.ActivePaneID()})
	st = a.activeBrowse()
	if st == nil {
		t.Fatal("browse state missing after refresh")
	}
	if st.page != 1 || st.done {
		t.Fatalf("walk after refresh = page %d done %v, want page 1 not done", st.page, st.done)
	}
	if st.after != nil {
		t.Fatalf("cursor after refresh = %v, want nil", st.after)
	}
	if st.loaded != 2 {
		t.Fatalf("loaded after refresh = %d, want 2", st.loaded)
	}
	if got := a.pane().Base().NumRows(); got != 2 {
		t.Fatalf("base frame rows after refresh = %d, want 2", got)
	}
}

// TestBrowsePagingNoBackendIsNoop pins that a non-browse backend (plain
// db.Backend without BrowsePage) makes ">" a no-op (nil cmd) instead of
// panicking.
func TestBrowsePagingNoBackendIsNoop(t *testing.T) {
	a := New(Options{
		Frames:  []reader.NamedFrame{{Name: "t", Frame: testFrame(2)}},
		Backend: fakeBackend{}, // no BrowsePage
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(ApplyFrameMsg{
		Frame:           testFrame(2),
		NewTab:          true,
		TabTitle:        "orders",
		SourceNamespace: "sales",
		SourceTable:     "orders",
	})
	_, cmd := a.Update(key(">"))
	if cmd != nil {
		t.Fatalf("ActNextPage with a non-browse backend returned a command, want nil")
	}
}

// emptyBrowseBackend returns rows on the first page and an empty window on
// every subsequent page — the exhaustion case.
type emptyBrowseBackend struct {
	fakeBackend
	calls int
}

func (e *emptyBrowseBackend) BrowsePage(_ context.Context, _, _ string, req *db.BrowsePageRequest) (*db.BrowsePageResult, error) {
	e.calls++
	if req.Page == 1 {
		frame := data.New("id", "name", "comment")
		frame.AppendRow([]any{"1", "alice", "c"})
		return &db.BrowsePageResult{Frame: frame, Total: 1, TotalFound: true, SortColumn: "id"}, nil
	}
	// Empty window: the walk is exhausted.
	return &db.BrowsePageResult{Frame: data.New("id", "name", "comment"), Total: 1, TotalFound: true, SortColumn: "id"}, nil
}

// TestBrowsePagingEmptyWindowExhausts pins that an empty window marks the
// walk done and returns a "已是最后一页" toast instead of panicking on
// Row(-1). Pre-fix, browsePageApplied accessed window.Row(NumRows()-1)
// unconditionally, crashing when the window had zero rows.
func TestBrowsePagingEmptyWindowExhausts(t *testing.T) {
	bb := &emptyBrowseBackend{}
	a := New(Options{
		Frames:  []reader.NamedFrame{{Name: "t", Frame: testFrame(1)}},
		Backend: bb,
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(ApplyFrameMsg{
		Frame:           testFrame(1),
		NewTab:          true,
		TabTitle:        "orders",
		SourceNamespace: "sales",
		SourceTable:     "orders",
	})

	// Page 2 returns an empty window.
	_, cmd := a.Update(key(">"))
	if cmd == nil {
		t.Fatal("ActNextPage returned no command")
	}
	msg := cmd()
	pm, ok := msg.(BrowsePageMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want BrowsePageMsg", msg)
	}

	// Applying the empty window must not panic and must mark done.
	// setToast returns a tea.Tick cmd (blocks when called), so we verify
	// the command is non-nil without executing it.
	_, toastCmd := a.Update(pm)
	if toastCmd == nil {
		t.Fatal("browsePageApplied returned no toast command for empty window")
	}

	st := a.activeBrowse()
	if !st.done {
		t.Fatal("walk not marked done after empty window")
	}
}

// ctxProbeBackend records ctx.Err() as seen inside BrowsePage: the pre-fix
// ActNextPage deferred cancel() in dispatch scope, so the runtime executed
// the Cmd with an already-cancelled context and every ">" failed with
// "context canceled" — the green tests hid it because the fake ignored ctx.
type ctxProbeBackend struct {
	fakeBackend
	ctxErr error
}

func (f *ctxProbeBackend) BrowsePage(ctx context.Context, _, _ string, req *db.BrowsePageRequest) (*db.BrowsePageResult, error) {
	f.ctxErr = ctx.Err()
	frame := data.New("id", "name", "comment")
	frame.AppendRow([]any{"1", "a", "c"})
	return &db.BrowsePageResult{Frame: frame, Total: 1, TotalFound: true, SortColumn: "id"}, nil
}

// TestBrowsePageContextAliveInsideCmd pins that the browse context is live
// when the runtime actually runs the Cmd, not just when dispatch returns it.
func TestBrowsePageContextAliveInsideCmd(t *testing.T) {
	bb := &ctxProbeBackend{}
	a := New(Options{
		Frames:  []reader.NamedFrame{{Name: "t", Frame: testFrame(1)}},
		Backend: bb,
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(ApplyFrameMsg{
		Frame:           testFrame(1),
		NewTab:          true,
		TabTitle:        "orders",
		SourceNamespace: "sales",
		SourceTable:     "orders",
	})
	_, cmd := a.Update(key(">"))
	if cmd == nil {
		t.Fatal("ActNextPage returned no command")
	}
	msg := cmd()
	pm, ok := msg.(BrowsePageMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want BrowsePageMsg", msg)
	}
	if pm.err != nil {
		t.Fatalf("BrowsePage error: %v", pm.err)
	}
	if bb.ctxErr != nil {
		t.Fatalf("context was already cancelled when the Cmd ran: %v", bb.ctxErr)
	}
}

// TestBrowsePagingSameTableTwoTabs pins that two tabs on the SAME table carry
// independent walk cursors. The browse map used to key on ns|table only, so
// opening the table twice overwrote the state: tab A's ">" advanced the shared
// cursor, and tab B's next page (or A's, after switching) jumped — cross-tab
// cursor corruption with no error. The key now includes the pane id.
func TestBrowsePagingSameTableTwoTabs(t *testing.T) {
	bb := &fakeBrowseBackend{}
	a := New(Options{
		Frames:  []reader.NamedFrame{{Name: "t", Frame: testFrame(2)}},
		Backend: bb,
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Two tabs on the same table (New() seeds one tab from Frames, so the two
	// ApplyFrameMsg calls land as tabs 1 and 2; tab 0 is the plain "t" frame).
	a.Update(ApplyFrameMsg{
		Frame:           testFrame(2),
		NewTab:          true,
		TabTitle:        "orders",
		SourceNamespace: "sales",
		SourceTable:     "orders",
	})
	a.Update(ApplyFrameMsg{
		Frame:           testFrame(2),
		NewTab:          true,
		TabTitle:        "orders",
		SourceNamespace: "sales",
		SourceTable:     "orders",
	})
	if len(a.tabs) != 3 {
		t.Fatalf("tabs = %d, want 3 (1 seeded + 2 table tabs)", len(a.tabs))
	}

	// The second tab is active; its state must be its own, page 1, not done —
	// NOT the first tab's state re-seeded under the same key.
	st2 := a.activeBrowse()
	if st2 == nil {
		t.Fatal("second tab's browse state not seeded")
	}
	if st2.page != 1 || st2.done {
		t.Fatalf("second tab walk = page %d done %v, want fresh page 1", st2.page, st2.done)
	}
	if st2.paneID != a.pane().ID() {
		t.Fatalf("second tab state paneID = %d, want active pane %d", st2.paneID, a.pane().ID())
	}

	// Advance tab 2, then switch to tab 1: tab 1's walk must still be at its
	// own page 1 — the shared-key overwrite used to make this state stale or
	// advanced.
	_, cmd := a.Update(key(">"))
	if cmd == nil {
		t.Fatal("ActNextPage returned no command on tab 2")
	}
	msg := cmd()
	pm, ok := msg.(BrowsePageMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want BrowsePageMsg", msg)
	}
	a.Update(pm)

	// Switch back to the FIRST table tab (tab 1; tab 0 is the seeded plain
	// "t" frame with no table source). Its walk must still be at its own
	// page 1 — the shared-key overwrite used to make this state stale or
	// advanced.
	a.Update(JumpToTabMsg{Index: 1})
	st1 := a.activeBrowse()
	if st1 == nil {
		t.Fatal("first tab's browse state vanished after tab 2 advanced")
	}
	if st1.paneID == st2.paneID {
		t.Fatal("both tabs share one browse state: key collision")
	}
	if st1.page != 1 || st1.done {
		t.Fatalf("first tab walk = page %d done %v, want untouched page 1", st1.page, st1.done)
	}
}

// TestCloseTabWithdrawsBrowseState pins the ST-05 per-tab inverse: closing a
// table tab removes the browse window it seeded. The connection itself is
// app-level and must survive (other tabs still use it), so the inverse is the
// per-tab registration, not the connection.
func TestCloseTabWithdrawsBrowseState(t *testing.T) {
	bb := &fakeBrowseBackend{}
	a := New(Options{
		Frames:  []reader.NamedFrame{{Name: "t", Frame: testFrame(2)}},
		Backend: bb,
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.Update(ApplyFrameMsg{
		Frame:           testFrame(2),
		NewTab:          true,
		TabTitle:        "orders",
		SourceNamespace: "sales",
		SourceTable:     "orders",
	})

	closed := a.pane()
	key := a.browseKey(closed)
	if a.browse[key] == nil {
		t.Fatal("browse state not seeded before close")
	}

	a.Update(CloseTabMsg{Index: 1})
	if _, ok := a.browse[key]; ok {
		t.Fatal("closing a tab left its browse window registered")
	}
	// The app-level connection is untouched by a single-tab close.
	if a.backend == nil {
		t.Fatal("single-tab close must not drop the app-level connection")
	}
}
