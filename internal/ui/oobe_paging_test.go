package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/reader"
)

// fakePagedBackend is a db.Backend + db.Paged stand-in for the OpenObserve
// pager path: the paging state is set directly instead of flowing through a
// real SSE stream, so the end-of-data probe can be pinned deterministically.
type fakePagedBackend struct {
	fakeBackend
	page     int
	pageSize int
	lastHits int
	fetched  bool
}

func (f *fakePagedBackend) GetPage() int { return f.page }
func (f *fakePagedBackend) SetPage(n int) {
	if n >= 0 {
		f.page = n
	}
}
func (f *fakePagedBackend) GetPageSize() int { return f.pageSize }
func (f *fakePagedBackend) SetPageSize(n int) {
	if n > 0 {
		f.pageSize = n
	}
}
func (f *fakePagedBackend) LastHits() int { return f.lastHits }
func (f *fakePagedBackend) Fetched() bool { return f.fetched }

// newOOBEPagerApp wires a fake Paged backend into an app with one table pane
// (dispatch gates on a live pane + frame). The refreshtable factory is
// stubbed so a page turn is observable as a call count instead of depending on
// the popup package's registration, which the ui test binary does not have.
func newOOBEPagerApp(b *fakePagedBackend, t *testing.T) (*App, *int) {
	t.Helper()
	refreshCalls := 0
	prev, had := Factories["refreshtable"]
	Factories["refreshtable"] = func(AppContext, string) (Overlay, error) {
		refreshCalls++
		return nil, nil
	}
	t.Cleanup(func() {
		if had {
			Factories["refreshtable"] = prev
		} else {
			delete(Factories, "refreshtable")
		}
	})
	a := New(Options{
		Frames:  []reader.NamedFrame{{Name: "t", Frame: testFrame(2)}},
		Backend: b,
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return a, &refreshCalls
}

// TestOOBENextPageAdvancesOnFullPage pins that a full page still walks forward:
// a page that exactly fills the page size cannot be the last one from the
// probe's point of view.
func TestOOBENextPageAdvancesOnFullPage(t *testing.T) {
	b := &fakePagedBackend{page: 0, pageSize: 200, lastHits: 200, fetched: true}
	a, calls := newOOBEPagerApp(b, t)

	a.Update(key(">"))
	if b.page != 1 {
		t.Fatalf("page = %d after '>', want 1", b.page)
	}
	if *calls != 1 {
		t.Fatalf("refreshtable calls = %d, want 1", *calls)
	}
	if a.toastText != "" {
		t.Fatalf("toast = %q, want none", a.toastText)
	}
}

// TestOOBENextPageStopsOnShortPage pins the classic end-of-data probe: a page
// that came back short means the next offset is past the end.
func TestOOBENextPageStopsOnShortPage(t *testing.T) {
	b := &fakePagedBackend{page: 1, pageSize: 200, lastHits: 50, fetched: true}
	a, calls := newOOBEPagerApp(b, t)

	a.Update(key(">"))
	if b.page != 1 {
		t.Fatalf("page = %d after '>', want it unchanged at 1", b.page)
	}
	if *calls != 0 {
		t.Fatalf("refreshtable calls = %d, want 0", *calls)
	}
	if a.toastText != "已是最后一页" {
		t.Fatalf("toast = %q, want %q", a.toastText, "已是最后一页")
	}
}

// TestOOBENextPageStopsOnEmptyPage pins the unbounded-walk fix: after the last
// full page the next fetch returns 0 rows, and that empty page must end the
// walk instead of letting '>' spin on SSE round trips forever.
func TestOOBENextPageStopsOnEmptyPage(t *testing.T) {
	b := &fakePagedBackend{page: 2, pageSize: 200, lastHits: 0, fetched: true}
	a, calls := newOOBEPagerApp(b, t)

	a.Update(key(">"))
	if b.page != 2 {
		t.Fatalf("page = %d after '>', want it unchanged at 2", b.page)
	}
	if *calls != 0 {
		t.Fatalf("refreshtable calls = %d, want 0", *calls)
	}
	if a.toastText != "已是最后一页" {
		t.Fatalf("toast = %q, want %q", a.toastText, "已是最后一页")
	}
}

// TestOOBENextPageAllowedBeforeFirstFetch pins that the probe's initial state
// (0 hits, no fetch yet) must not block the first page turn.
func TestOOBENextPageAllowedBeforeFirstFetch(t *testing.T) {
	b := &fakePagedBackend{page: 0, pageSize: 200, lastHits: 0, fetched: false}
	a, calls := newOOBEPagerApp(b, t)

	a.Update(key(">"))
	if b.page != 1 {
		t.Fatalf("page = %d after '>', want 1", b.page)
	}
	if *calls != 1 {
		t.Fatalf("refreshtable calls = %d, want 1", *calls)
	}
	if a.toastText != "" {
		t.Fatalf("toast = %q, want none", a.toastText)
	}
}
