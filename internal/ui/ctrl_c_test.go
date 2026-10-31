package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/theme"
)

// ctrlC builds the key message the key() helper cannot: a multi-key chord.
func ctrlC() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
}

// keyCatchOverlay records the last key it received; the ctrl+c layering tests
// use it to prove keys reach the top overlay instead of the app quitting
// underneath it.
type keyCatchOverlay struct{ got string }

func (o *keyCatchOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		o.got = k.String()
	}
	return o, nil
}

func (o *keyCatchOverlay) View(width, height int, th *theme.Theme) string { return "" }

// TestCtrlCLayering pins the ctrl+c priority: modal overlay > statement in
// flight > inline editors > quit. Pre-fix, Update intercepted ctrl+c before
// handleKey, so a confirm box's ctrl+c-to-cancel binding was dead and the
// app quit underneath the user's pending decision.
func TestCtrlCLayering(t *testing.T) {
	t.Run("overlay owns ctrl+c", func(t *testing.T) {
		a := newTestApp(t)
		ov := &keyCatchOverlay{}
		a.overlays = append(a.overlays, ov)
		_, cmd := a.Update(ctrlC())
		assertNotQuit(t, cmd, "ctrl+c must not quit while a modal overlay is open")
		if ov.got != "ctrl+c" {
			t.Fatalf("overlay received %q, want ctrl+c", ov.got)
		}
	})

	t.Run("bare ctrl+c still quits", func(t *testing.T) {
		a := newTestApp(t)
		_, cmd := a.Update(ctrlC())
		if cmd == nil {
			t.Fatal("bare ctrl+c must produce a command")
		}
		msg := cmd()
		if _, quit := msg.(tea.QuitMsg); !quit {
			t.Fatalf("bare ctrl+c returned %T, want QuitMsg", msg)
		}
	})

	t.Run("active search owns ctrl+c", func(t *testing.T) {
		a := newTestApp(t)
		a.Update(key("/")) // open the fuzzy search bar
		if !a.search.Active() {
			t.Fatal("search bar did not open on /")
		}
		_, cmd := a.Update(ctrlC())
		assertNotQuit(t, cmd, "ctrl+c must cancel the search, not quit")
		if a.search.Active() {
			t.Fatal("search bar still active after ctrl+c")
		}
	})
}

func assertNotQuit(t *testing.T, cmd tea.Cmd, what string) {
	t.Helper()
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		if _, quit := msg.(tea.QuitMsg); quit {
			t.Fatal(what)
		}
	}
}
