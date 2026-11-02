package ui

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/sqltui/internal/config"
	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	dbcore "github.com/LinPr/sqltui/internal/db/core"
	"github.com/LinPr/sqltui/internal/query"
	"github.com/LinPr/sqltui/internal/reader"
	"github.com/LinPr/sqltui/internal/theme"
)

const toastDuration = 2500 * time.Millisecond

// browsePageTimeout is the ceiling of one "next page" fetch, matching the
// kernel statement timeout.
const browsePageTimeout = 30 * time.Second

// Options configures a new App.
type Options struct {
	Frames  []reader.NamedFrame // initial tabs (already engine-registered)
	Engine  *query.Engine       // embedded SQL engine (nil in db mode)
	Backend db.Backend          // live SQL connection (nil in file mode)
	KV      db.KVBackend        // live redis connection (nil otherwise)

	ThemeName      string
	ShowBorders    bool
	ShowRowNumbers bool
}

// App is the root model: tabs of panes, an overlay stack, the search bar and
// the bottom status line.
type App struct {
	tabs     []*Pane
	active   int
	overlays []Overlay

	th        *theme.Theme
	themeName string

	showBorders    bool
	showRowNumbers bool

	width, height int

	toastText string
	toastID   int

	search SearchBar

	engine  *query.Engine
	backend db.Backend
	kv      db.KVBackend

	// pendingEdit/pendingDelete hold an in-flight cell edit / row delete that
	// a confirm popup is about to commit. They are set by the dispatch cases
	// (ActEdit for Agent E, ActDelete here) and cleared by the commit/cancel
	// built-ins (saveedit/canceledit/deleterows/canceldelete).
	pendingEdit   *PendingEdit
	pendingDelete *PendingDelete

	// schemaC is this app's completion-schema snapshot (see app_schema.go).
	// Created once in New; its own mutex guards every access.
	schemaC *schemaCache

	// syncedFrame caches the frame last registered as "_" (see ensureSynced).
	syncedFrame *data.Frame

	// browse remembers where each table tab's row window ends, so the "next
	// page" key continues the window instead of re-fetching the first one.
	browse map[string]*browseState
}

// browseState tracks one table tab's loaded window and the cursor that
// continues it.
type browseState struct {
	ns         string
	table      string
	paneID     int
	page       int      // OFFSET page (1-based)
	after      []string // keyset cursor at the end of the last window
	sortColumn string   // "" = OFFSET mode
	loaded     int
	total      uint64
	totalFound bool
	done       bool // the last window was short
}

// toastExpiredMsg clears a toast after its timer fires.
type toastExpiredMsg struct{ id int }

// New builds the app from options. Each frame becomes a tab.
func New(opts Options) *App {
	th, ok := theme.Builtin(opts.ThemeName)
	name := opts.ThemeName
	if !ok {
		th = theme.Default()
		name = th.Palette.Name
	}
	a := &App{
		th:             th,
		themeName:      name,
		showBorders:    opts.ShowBorders,
		showRowNumbers: opts.ShowRowNumbers,
		engine:         opts.Engine,
		backend:        opts.Backend,
		kv:             opts.KV,
		schemaC:        &schemaCache{},
		browse:         map[string]*browseState{},
		width:          80,
		height:         24,
	}
	for _, nf := range opts.Frames {
		a.tabs = append(a.tabs, NewPane(nf.Name, nf.Frame))
	}
	return a
}

// deleteKeysReadyMsg carries a resolved primary-key set back into the update
// loop, so the confirm overlay is staged from UI-thread state rather than from
// a catalog read inside it.
type deleteKeysReadyMsg struct {
	frame *data.Frame
	rows  []int
	table string
	ns    string
}

// onBackendChanged lets the db-mode integration drop per-connection caches
// when the app switches connections (the OpenObserve schema overlay captures
// the backend it was built with). dbmode registers it via SetOnBackendChanged;
// ui cannot import dbmode (the dependency runs the other way).
var onBackendChanged func()

// SetOnBackendChanged installs the backend-switch hook (nil clears it).
func SetOnBackendChanged(fn func()) { onBackendChanged = fn }

// Run drives the app to completion on the terminal. Whatever live
// connections the app owns are closed when the program returns, so every
// exit path (last tab closed, quit, crash-recoverable return) drops them.
func Run(app *App) (err error) {
	// defer 的注册顺序是执行的倒序：panic 时先跑 CloseConnections（注册在后、
	// 先执行），再由 recover 把 panic 转成带栈的错误。此前的顺序调用在 panic
	// 路径会跳过清理，与"every exit path drops them"矛盾；裸 panic 还会把
	// 终端留在备用屏里没有交代。
	defer func() {
		if r := recover(); r != nil {
			app.CloseConnections()
			err = fmt.Errorf("internal error: %v\n%s", r, debug.Stack())
		}
	}()
	defer app.CloseConnections()
	_, err = tea.NewProgram(app).Run()
	return err
}

// CloseConnections closes the live SQL backend and key-value connection the
// app currently holds (both nil-safe). The replacement path in SetBackendMsg
// uses the same policy: a superseded connection is closed immediately.
func (a *App) CloseConnections() {
	if a.backend != nil {
		_ = a.backend.Close() // best effort on teardown
		a.backend = nil
	}
	if a.kv != nil {
		_ = a.kv.Close()
		a.kv = nil
	}
}

// --- tea.Model ---------------------------------------------------------------

func (a *App) Init() tea.Cmd { return nil }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		return a, nil

	case PushOverlayMsg:
		if m.Overlay != nil {
			return a, a.pushOverlay(m.Overlay)
		}
		return a, nil

	case SetBackendMsg:
		var warm tea.Cmd
		if m.Backend != nil && m.Backend != a.backend {
			if a.backend != nil {
				_ = a.backend.Close() // superseded: a reconnect replaces it
			}
			a.backend = m.Backend
			// The completion cache pins the backend it was warmed against; a
			// new connection must rebind it and drop the stale snapshot, or
			// sheet type rows silently degrade and every warm fetch is
			// discarded until the query editor happens to re-warm it.
			a.schemaC.mu.Lock()
			a.schemaC.resetFor(m.Backend)
			a.schemaC.mu.Unlock()
			if onBackendChanged != nil {
				onBackendChanged()
			}
			// Warm the namespace off the update loop: the delete-confirm and
			// completion paths resolve it from the update loop, and on
			// postgres a cold lookup is a real query (30s on a dead server).
			if _, ok := a.backend.(db.NamespaceWarmer); ok {
				warm = func() tea.Msg {
					a.backend.(db.NamespaceWarmer).WarmNamespace()
					return nil
				}
			}
		}
		if m.KV != nil && m.KV != a.kv {
			if a.kv != nil {
				_ = a.kv.Close()
			}
			a.kv = m.KV
		}
		return a, warm

	case deleteKeysReadyMsg:
		a.pendingDelete = &PendingDelete{Frame: m.frame, Rows: m.rows, Table: m.table, Namespace: m.ns}
		return a, a.runCommand("confirmdelete", "")

	case CloseOverlayMsg:
		if n := len(a.overlays); n > 0 {
			a.overlays = a.overlays[:n-1]
		}
		return a, nil

	case ErrorMsg:
		a.overlays = append(a.overlays, errBox{err: m.Err})
		return a, nil

	case ToastMsg:
		return a, a.setToast(m.Text)

	case toastExpiredMsg:
		if m.id == a.toastID {
			a.toastText = ""
		}
		return a, nil

	case ApplyFrameMsg:
		return a, a.applyFrame(m)

	case BrowsePageMsg:
		return a, a.browsePageApplied(m)

	case ReplaceBaseMsg:
		return a, a.replaceBase(m)

	case RunCommandMsg:
		return a, a.runCommand(m.Name, m.Arg)

	case SetThemeMsg:
		th, ok := theme.Builtin(m.Name)
		if !ok {
			a.overlays = append(a.overlays, errBox{err: fmt.Errorf("unknown theme: %s", m.Name)})
			return a, nil
		}
		a.th, a.themeName = th, m.Name
		a.persistUI()
		return a, a.setToast("theme: " + m.Name)

	case ToggleBordersMsg:
		a.showBorders = !a.showBorders
		a.persistUI()
		return a, nil

	case ToggleRowNumbersMsg:
		a.showRowNumbers = !a.showRowNumbers
		a.persistUI()
		return a, nil

	case ResetStackMsg:
		if p := a.pane(); p != nil {
			p.Reset()
		}
		return a, nil

	case JumpToRowMsg:
		if p := a.pane(); p != nil && p.Current() != nil {
			p.Table.JumpTo(m.Row, p.Current().NumRows())
		}
		return a, nil

	case JumpToTabMsg:
		if len(a.tabs) > 0 {
			a.active = clamp(m.Index, 0, len(a.tabs)-1)
		}
		return a, nil

	case CloseTabMsg:
		return a, a.closeTab(m.Index)

	case RegisterTableMsg:
		return a, a.registerTable(m.Name)

	case ExecProcessMsg:
		if m.Cmd == nil {
			return a, nil
		}
		return a, tea.ExecProcess(m.Cmd, tea.ExecCallback(m.OnDone))

	case OobeColsSelectedMsg:
		if p := a.pane(); p != nil && a.backend != nil {
			f := p.Current()
			if f != nil {
				cols := make([]int, 0, len(m.Columns))
				for _, name := range m.Columns {
					for i, col := range f.Columns {
						if col.Name == name {
							cols = append(cols, i)
							break
						}
					}
				}
				if len(cols) > 0 {
					p.TableCols = cols
					p.HScroll = 0
				}
			}
		}
		return a, nil

	case SetQueryParamsMsg:
		if a.backend != nil {
			if oo, ok := a.backend.(db.QueryParamsSetter); ok {
				needsReset := m.Minutes != oo.GetTimeRange()
				oo.SetTimeRange(m.Minutes)
				page := m.Page
				if needsReset {
					page = 0
				}
				oo.SetPage(page)
				oo.SetPageSize(m.PageSize)
				if needsReset {
					if tw, ok2 := a.backend.(db.TimeWindowResetter); ok2 {
						tw.ResetTimeWindow()
					}
				}
				return a, tea.Batch(
					a.runCommand("refreshtable", ""),
					a.setToast(fmt.Sprintf("time: %d min  p%d  size: %d", m.Minutes, page+1, m.PageSize)),
				)
			}
		}
		return a, nil

	case columnMetaMsg:
		// An async ColumnsMeta fetch finished. On success store the metadata
		// into the completion cache so the next View shows the real type
		// instead of the frame DType fallback. The cache may be unbound when
		// WarmCompletionSchema never ran (cold open); bind it to the current
		// backend in that case. A cache bound to a different backend
		// (connection swapped mid-flight) rejects the store.
		if m.err == nil && m.meta != nil && a.backend != nil {
			c := a.completionCache()
			c.mu.Lock()
			if c.backend == nil {
				c.backend = a.backend
				c.ns = make(map[string]string)
				c.cols = make(map[string][]string)
				c.meta = make(map[string][]db.ColumnMeta)
				c.fetched = make(map[string]bool)
			}
			if c.backend == a.backend {
				c.meta[m.table] = m.meta
				c.fetched[m.table] = true
				if c.cols[m.table] == nil {
					cols := make([]string, len(m.meta))
					for i, cm := range m.meta {
						cols[i] = cm.Name
					}
					c.cols[m.table] = cols
				}
			}
			c.mu.Unlock()
		}
		return a, nil

	case tea.PasteMsg:
		// Bracketed paste follows key routing: the top overlay swallows it;
		// with no overlay open it feeds the active search bar.
		if n := len(a.overlays); n > 0 {
			ov, cmd := a.overlays[n-1].Update(msg)
			a.overlays[n-1] = ov
			return a, cmd
		}
		if a.search.Active() {
			a.search.Paste(m.Content)
			if p := a.pane(); p != nil {
				p.Table.ClampTo(a.search.Preview())
			}
		}
		return a, nil

	case cellSavedMsg:
		// An async UPDATE finished. On success toast and refresh the table so
		// the edit is visible; on error surface the error overlay.
		if m.err != nil {
			a.overlays = append(a.overlays, errBox{err: m.err})
			return a, nil
		}
		cmds := []tea.Cmd{a.setToast(fmt.Sprintf("updated %d row(s)", m.rows))}
		if a.backend != nil {
			cmds = append(cmds, a.runCommand("refreshtable", ""))
		}
		return a, tea.Batch(cmds...)

	case rowsDeletedMsg:
		// An async DELETE finished. On success toast, refresh the table and
		// clear the multi-select set; on error surface the error overlay.
		if m.err != nil {
			a.overlays = append(a.overlays, errBox{err: m.err})
			return a, nil
		}
		if p := a.pane(); p != nil {
			p.Table.ClearSelect()
		}
		cmds := []tea.Cmd{a.setToast(fmt.Sprintf("deleted %d row(s)", m.rows))}
		if a.backend != nil {
			cmds = append(cmds, a.runCommand("refreshtable", ""))
		}
		return a, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		// ctrl+c layering: a modal overlay owns it first (confirm boxes bind
		// it to cancel, the table-fetch overlay cancels the fetch and closes
		// itself — quitting from under one would throw the user's pending
		// decision away). Next the statement in flight: the query editor
		// closes as soon as a statement is sent, so a running query has no
		// other handle. Then active inline editors (search bar, sheet cell
		// edit / filter) bind ctrl+c to cancel themselves. With nothing
		// modal, editing, or running, it quits, as before.
		if m.String() == "ctrl+c" {
			if n := len(a.overlays); n > 0 {
				return a.handleKey(m)
			}
			if c, ok := a.backend.(db.Canceler); ok && c.CancelRunning() {
				return a, a.setToast("query canceled")
			}
			if p := a.pane(); a.search.Active() || (p != nil && (p.SheetEditing || p.SheetFiltering)) {
				return a.handleKey(m)
			}
			return a, tea.Quit
		}
		return a.handleKey(m)

	default:
		// Anything else (ticks, async results, ...) goes to the top overlay
		// so self-updating overlays keep working.
		if n := len(a.overlays); n > 0 {
			ov, cmd := a.overlays[n-1].Update(msg)
			a.overlays[n-1] = ov
			return a, cmd
		}
		return a, nil
	}
}

func (a *App) View() tea.View {
	w, h := a.width, a.height
	bodyH := max(1, h-1)

	// When search is active, reserve one line at the top for the search bar
	// and shrink the body accordingly. The status bar stays at the bottom.
	var topBar string
	if a.search.Active() {
		topBar = a.search.View(w, a.th) + "\n"
		bodyH = max(1, bodyH-1)
	}

	bottom := a.renderBottom(w)

	var body string
	if n := len(a.overlays); n > 0 {
		top := a.overlays[n-1]
		box := top.View(w, bodyH, a.th)
		if fs, ok := top.(FullscreenOverlay); ok && fs.Fullscreen() {
			body = FillPage(box, w, bodyH) + "\n" + bottom
		} else {
			// When a non-fullscreen popup sits on top of a fullscreen overlay
			// (e.g. time-range selector over the dashboard panel viewer), use
			// the nearest fullscreen overlay below as the background so the
			// popup floats over it instead of over the empty main content.
			bg := a.renderBody(w, bodyH)
			for i := n - 2; i >= 0; i-- {
				if fs2, ok2 := a.overlays[i].(FullscreenOverlay); ok2 && fs2.Fullscreen() {
					bg = FillPage(a.overlays[i].View(w, bodyH, a.th), w, bodyH)
					break
				}
			}
			body = Composite(bg+"\n"+bottom, box, w, bodyH+1)
		}
	} else {
		body = a.renderBody(w, bodyH) + "\n" + bottom
	}

	v := tea.NewView(topBar + body)
	v.AltScreen = true
	return v
}

// --- rendering ----------------------------------------------------------------

func (a *App) renderBody(w, h int) string {
	p := a.pane()
	if p == nil {
		msg := "no data loaded"
		if a.backend != nil || a.kv != nil {
			msg = "no tables open - press : for commands"
		}
		lines := strings.Split(blankLines(w, h, a.th), "\n")
		mid := h / 2
		if mid < len(lines) {
			pad := max(0, (w-len(msg))/2)
			lines[mid] = a.th.Subtle.Render(padLine(strings.Repeat(" ", pad)+msg, w))
		}
		return strings.Join(lines, "\n")
	}

	frame := p.Current()
	if a.search.Active() {
		frame = a.search.Preview()
	}

	if p.Mode == ModeJsonView && frame != nil && frame.NumRows() > 0 {
		return renderJsonView(frame, p.Table.Sel(), p.JsonViewOff, w, h, a.th)
	}

	if p.Mode == ModeSheet && frame != nil && frame.NumRows() > 0 {
		if p.SheetEditing {
			keyW, valW := sheetTableGeometry(frame, w)
			matched := sheetMatchedCols(frame, p.SheetFilter.String())
			actualCol := p.SheetField
			if len(matched) > 0 {
				actualCol = matched[clamp(p.SheetField, 0, len(matched)-1)]
			}
			leftContent := renderSheetKeyPanel(frame, p.Table.Sel(), p.SheetOff, keyW, h, p.SheetField,
				p.SheetFilter.String(), p.SheetFiltering, a.th)
			rightContent := renderSheetDrawer(frame, p.Table.Sel(), actualCol,
				[]rune(p.SheetEdit.String()), p.SheetEdit.Cursor(), p.SheetValOff, valW, h, a.cursorFieldType(), a.th)
			return joinDrawerSplit(leftContent, rightContent, keyW, valW, h, a.th)
		}
		return renderSheet(frame, p.Table.Sel(), p.SheetOff, w, h, p.SheetField,
			false, nil, 0, p.SheetValOff,
			p.SheetFilter.String(), p.SheetFiltering, a.th)
	}

	return p.Table.Render(frame, RenderOpts{
		Width:          w,
		Height:         h,
		Theme:          a.th,
		ShowBorders:    a.showBorders,
		ShowRowNumbers: a.showRowNumbers,
		HeaderCur:      p.Table.ColCursor(),
		SeverityCol:    a.severityColName(),
		TableCols:      p.TableCols,
		NoEllipsis:     len(p.TableCols) > 0,
		HScroll:        p.HScroll,
		HighlightQuery: a.searchQuery(),
		HighlightCols:  p.TableCols,
	})
}

func (a *App) renderBottom(w int) string {
	if a.toastText != "" {
		return a.th.Success.Render(padLine(" "+a.toastText, w))
	}
	info := statusInfo{Tab: 0, NTabs: len(a.tabs), Row: 0}
	if p := a.pane(); p != nil {
		info.Tab = a.active + 1
		info.Crumbs = p.Crumbs()
		if f := p.Current(); f != nil {
			info.Row = p.Table.Sel() + 1
			info.NRows = f.NumRows()
			info.NCols = f.NumCols()
			if f.NumCols() > 0 {
				info.Mode = "fit"
				if p.Table.Expanded() {
					info.Mode = "wide"
				}
				// The column cursor is live in both modes (y-copy targets it).
				info.Col = f.Columns[clamp(p.Table.ColCursor(), 0, f.NumCols()-1)].Name
			}
		}
	}
	switch {
	case a.backend != nil:
		info.Conn = a.backend.Title()
		// Populate OpenObserve-specific status info via type assertion.
		if oobe, ok := a.backend.(db.QueryParamsSetter); ok {
			info.TimeRange = formatTimeRange(oobe.GetTimeRange())
			info.Page = oobe.GetPage() + 1
			info.PageSize = oobe.GetPageSize()
		}
	case a.kv != nil:
		info.Conn = a.kv.Title()
	}
	return renderStatusBar(info, w, a.th)
}

// severityColName returns the severity column name when connected to OpenObserve.
// Returns "" otherwise (disables severity coloring).
func (a *App) severityColName() string {
	if a.backend != nil && a.backend.Kind() == "openobserve" {
		return "severity"
	}
	return ""
}

// --- key handling ---------------------------------------------------------------

func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Modal overlays swallow every key.
	if n := len(a.overlays); n > 0 {
		ov, cmd := a.overlays[n-1].Update(msg)
		a.overlays[n-1] = ov
		return a, cmd
	}

	if a.search.Active() {
		return a.handleSearchKey(msg)
	}

	key := msg.String()
	if key == "ctrl+c" {
		return a, tea.Quit
	}

	p := a.pane()
	if p != nil && p.Mode == ModeJsonView {
		switch key {
		case "j", "down":
			p.JsonViewOff++
		case "k", "up":
			if p.JsonViewOff > 0 {
				p.JsonViewOff--
			}
		case "q", "esc":
			p.Mode = ModeSheet
		}
		return a, nil
	}
	if p != nil && p.Mode == ModeSheet {
		// While inline-editing a sheet field, keys go to the edit handler
		// before the normal SheetBindings dispatch: j/k and other nav keys
		// must not move the cursor mid-edit.
		if p.SheetEditing {
			return a.handleSheetEditKey(msg)
		}
		// While the field-filter input is active, keys go to the filter
		// handler instead of the normal dispatch so "/" can pre-type the
		// first rune and esc clears the filter instead of leaving sheet mode.
		if p.SheetFiltering {
			return a.handleSheetFilterKey(msg)
		}
		return a, a.dispatch(actionFor(key, SheetBindings, GlobalBindings), key)
	}
	// space/p opens unified query-params popup for OpenObserve backends
	if p != nil && p.Mode == ModeTable && (key == "space" || key == "p") {
		if a.backend != nil {
			if _, ok := a.backend.(db.TimeRanger); ok {
				return a, a.runCommand("queryparams", "")
			}
		}
	}

	// In column zoom mode (f key), left/right do character-level scroll.
	if p != nil && p.Mode == ModeTable && p.ColZoomed {
		const hStep = 10
		switch key {
		case "l", "right":
			p.HScroll += hStep
			if max := hScrollMax(p); max > 0 && p.HScroll > max {
				p.HScroll = max
			}
			return a, nil
		case "h", "left":
			p.HScroll -= hStep
			if p.HScroll < 0 {
				p.HScroll = 0
			}
			return a, nil
		}
	}

	return a, a.dispatch(actionFor(key, TableBindings, GlobalBindings), key)
}

// hScrollMax is the widest cell of the pane's last visible column (the one
// HScroll pans within), past which scrolling right shows nothing. Zero — a
// pane with no frame or no TableCols — leaves the offset unclamped upward.
func hScrollMax(p *Pane) int {
	f := p.Current()
	if f == nil || len(p.TableCols) == 0 {
		return 0
	}
	col := p.TableCols[len(p.TableCols)-1]
	if col < 0 || col >= f.NumCols() {
		return 0
	}
	max := 0
	for i := 0; i < f.NumRows(); i++ {
		if w := ansi.StringWidth(f.CellString(i, col)); w > max {
			max = w
		}
	}
	return max
}

// handleSheetFilterKey routes keys while the sheet field-filter input is
// active. esc/ctrl+c clear the filter and exit filter input; enter commits the
// current pattern (filter stays applied, input closes); backspace pops the
// last rune; ctrl+u clears the text; up/down/j/k move the cursor within the
// matched set; printable runes append and reset the cursor to the first
// match. After each change the matched set is recomputed and SheetField is
// clamped, with the left list edge-followed.
func (a *App) handleSheetFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := a.pane()
	if p == nil {
		return a, nil
	}
	f := p.Current()
	if f == nil {
		return a, nil
	}
	key := msg.String()
	switch key {
	case "esc", "ctrl+c":
		p.SheetFiltering = false
		p.SheetFilter = LineInput{}
		p.SheetField = 0
		p.SheetOff = 0
		return a, nil
	case "enter":
		p.SheetFiltering = false
		// Keep the filter applied. Clamp the cursor to the matched set and
		// edge-follow so the entry stays visible.
		a.clampSheetToMatched(f, p)
		return a, nil
	case "backspace", "ctrl+h":
		p.SheetFilter.Backspace()
		p.SheetField = 0
		p.SheetOff = 0
		a.clampSheetToMatched(f, p)
		return a, nil
	case "ctrl+u":
		p.SheetFilter.Clear()
		p.SheetField = 0
		p.SheetOff = 0
		return a, nil
	case "up", "k":
		if p.SheetField > 0 {
			p.SheetField--
		}
		p.SheetValOff = 0
		a.edgeFollowSheet(f, p)
		return a, nil
	case "down", "j":
		matched := sheetMatchedCols(f, p.SheetFilter.String())
		if len(matched) > 0 && p.SheetField < len(matched)-1 {
			p.SheetField++
		}
		p.SheetValOff = 0
		a.edgeFollowSheet(f, p)
		return a, nil
	default:
		if t := msg.Key().Text; t != "" && msg.Mod == 0 {
			p.SheetFilter.InsertKey(t)
			p.SheetField = 0
			p.SheetOff = 0
			a.clampSheetToMatched(f, p)
		}
		return a, nil
	}
}

// clampSheetToMatched ensures SheetField is a valid index into the current
// matched set and re-runs the left-list edge-follow. Called after every
// filter mutation that can shrink the matched set.
func (a *App) clampSheetToMatched(f *data.Frame, p *Pane) {
	matched := sheetMatchedCols(f, p.SheetFilter.String())
	if len(matched) == 0 {
		p.SheetField = 0
		p.SheetOff = 0
		return
	}
	if p.SheetField >= len(matched) {
		p.SheetField = len(matched) - 1
	}
	if p.SheetField < 0 {
		p.SheetField = 0
	}
	a.edgeFollowSheet(f, p)
}

// edgeFollowSheet advances SheetOff so the cursored entry stays visible in
// the sheet body, mirroring renderSheet's geometry exactly.
func (a *App) edgeFollowSheet(f *data.Frame, p *Pane) {
	matched := sheetMatchedCols(f, p.SheetFilter.String())
	if len(matched) == 0 {
		p.SheetOff = 0
		return
	}
	keyW, valW := sheetTableGeometry(f, a.width)
	bodyH := max(1, a.height-1)
	// Mirror renderSheet's header accounting: row header + col header + footer
	// hint = 3. Add 1 more when the filter line is visible.
	headers := 3
	if p.SheetFiltering || strings.TrimSpace(p.SheetFilter.String()) != "" {
		headers = 4
	}
	visible := bodyH - headers
	if visible < 1 {
		visible = 1
	}
	dataRow := p.Table.Sel()
	// Single Θ(m) scan builds a prefix sum; count/offset/end are all O(1)
	// (SICP-style memoization).
	heights := sheetBodyEntryHeightsCols(f, matched, dataRow, keyW, valW)
	total := heights[len(matched)]
	curStart := heights[p.SheetField]
	curEnd := heights[p.SheetField+1] - 1
	maxOff := max(0, total-visible)
	if curStart < p.SheetOff {
		p.SheetOff = curStart
	} else if curEnd >= p.SheetOff+visible {
		p.SheetOff = curEnd - visible + 1
		if p.SheetOff > curStart {
			p.SheetOff = curStart
		}
	}
	p.SheetOff = clamp(p.SheetOff, 0, maxOff)
}

// handleSheetEditKey routes keys while a sheet field is being inline-edited.
// esc/ctrl+c cancel (keeping the original value); ctrl+s commits via the
// "confirmsave" command; printable runes insert at the cursor; backspace
// deletes before the cursor; left/right/home/end/ctrl+a/ctrl+e move the
// cursor; ctrl+u clears the buffer. After each mutation the right-pane
// value scroll is re-clamped. Any other key is swallowed so the field does
// not move.
func (a *App) handleSheetEditKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := a.pane()
	if p == nil {
		return a, nil
	}
	f := p.Current()
	if f == nil {
		return a, nil
	}
	key := msg.String()
	switch key {
	case "esc", "ctrl+c":
		p.SheetEditing = false
		p.SheetEdit = LineInput{}
		return a, nil
	case "ctrl+s":
		// Same matched-set resolution as ActEdit: the UPDATE must name the
		// column the user was looking at, not the frame's raw index.
		col := sheetActiveCol(f, p.SheetField, p.SheetFilter.String())
		if col < 0 {
			return a, nil
		}
		a.pendingEdit = &PendingEdit{
			Frame:     f,
			Row:       p.Table.Sel(),
			Col:       col,
			ColName:   f.Columns[col].Name,
			OldValue:  f.CellString(p.Table.Sel(), col),
			NewValue:  p.SheetEdit.String(),
			Table:     a.BaseCrumb(),
			Namespace: a.CurrentTableNamespace(),
		}
		return a, a.runCommand("confirmsave", "")
	default:
		// The shared line-input machine covers backspace/delete/cursor moves/
		// ctrl+u and unmodified printable inserts; every other key is
		// swallowed so the field does not move.
		p.SheetEdit.Handle(msg)
	}
	// Re-clamp the right-pane value scroll to the edit buffer length.
	_, valW := sheetTableGeometry(f, a.width)
	// drawer header: fieldname(type) + separator + hint = 3 lines
	visible := max(1, max(1, a.height-1)-3)
	editCol := sheetActiveCol(f, p.SheetField, p.SheetFilter.String())
	count := 0
	if editCol >= 0 {
		count = sheetValueLineCount(f, p.Table.Sel(), editCol, valW)
	}
	if editing := p.SheetEdit.Len(); editing > 0 {
		count = strings.Count(ansi.Wrap(p.SheetEdit.String(), valW, ""), "\n") + 1
	}
	maxOff := max(0, count-visible)
	// Keep the cursor line in view while typing.
	curLine, _ := sheetEditLineOf([]rune(p.SheetEdit.String()), p.SheetEdit.Cursor(), valW)
	if curLine < p.SheetValOff {
		p.SheetValOff = curLine
	} else if curLine >= p.SheetValOff+visible {
		p.SheetValOff = curLine - visible + 1
	}
	p.SheetValOff = clamp(p.SheetValOff, 0, maxOff)
	return a, nil
}

func (a *App) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		a.search.Cancel()
		if p := a.pane(); p != nil {
			p.Table.ClampTo(p.Current())
		}
		return a, nil
	case "enter":
		f, crumb, ok := a.search.Commit()
		if p := a.pane(); ok && p != nil {
			p.Push(f, crumb)
		} else if p != nil {
			p.Table.ClampTo(p.Current())
		}
		return a, nil
	case "backspace":
		a.search.Backspace()
	default:
		if t := msg.Key().Text; t != "" {
			a.search.Type(t)
		}
	}
	if p := a.pane(); p != nil {
		p.Table.ClampTo(a.search.Preview())
	}
	return a, nil
}

// dispatch executes a semantic action from the keymap.
func (a *App) dispatch(action, key string) tea.Cmd {
	p := a.pane()
	var f *data.Frame
	if p != nil {
		f = p.Current()
	}
	nrows, ncols := 0, 0
	if f != nil {
		nrows, ncols = f.NumRows(), f.NumCols()
	}

	switch action {
	// global
	case ActQuit:
		return tea.Quit
	case ActHelp:
		return a.runCommand("help", "")
	case ActPalette:
		return a.runCommand("palette", "")
	case ActTabSwitch:
		return a.runCommand("tabs", "")
	case ActPrevTab:
		if len(a.tabs) > 1 {
			a.active = (a.active - 1 + len(a.tabs)) % len(a.tabs)
		}
	case ActNextTab:
		if len(a.tabs) > 1 {
			a.active = (a.active + 1) % len(a.tabs)
		}
	case ActEscBack:
		// Handled before the pane check: on an empty db-mode workspace esc
		// reopens the connection form.
		return a.escBack()
	}

	if p == nil || f == nil {
		return nil
	}

	switch action {
	// table navigation
	case ActUp:
		p.Table.Move(-1, nrows)
	case ActDown:
		p.Table.Move(1, nrows)
	case ActTop:
		p.Table.Top()
	case ActBottom:
		p.Table.Bottom(nrows)
	case ActHalfUp:
		p.Table.HalfPageUp(nrows)
	case ActHalfDown:
		p.Table.HalfPageDown(nrows)
	case ActPageUp:
		p.Table.PageUp(nrows)
	case ActPageDown:
		p.Table.PageDown(nrows)
	case ActRandom:
		p.Table.Random(nrows)
	case ActLeft:
		p.Table.PrevCol(ncols)
	case ActRight:
		p.Table.NextCol(ncols)
	case ActFirstCol:
		p.Table.FirstCol()
	case ActLastCol:
		p.Table.LastCol(ncols)
	case ActExpand:
		p.Table.ToggleExpanded()
	case ActEdit:
		// Toggle inline edit on the cursored sheet field. Only meaningful in
		// sheet mode; in table mode e is unbound. While already editing, e is
		// a no-op (esc cancels, ctrl+s commits).
		if p.Mode != ModeSheet || f == nil || nrows == 0 || ncols == 0 {
			return nil
		}
		if p.SheetEditing {
			return nil
		}
		// SheetField is an index into the filter's matched set, not the frame:
		// editing the raw index while a filter is active would read (and then
		// ctrl+s write) a different column than the one on screen.
		col := sheetActiveCol(f, p.SheetField, p.SheetFilter.String())
		if col < 0 {
			return nil
		}
		val := f.CellString(p.Table.Sel(), col)
		p.SheetEdit = NewLineInput(val)
		p.SheetEditing = true
		p.SheetValOff = 0
		return nil
	case ActRefresh:
		if a.backend != nil {
			if tw, ok := a.backend.(db.TimeWindowResetter); ok {
				tw.ResetTimeWindow()
			}
			return a.runCommand("refreshtable", "")
		}
		if a.kv != nil {
			return a.setToast("use r in the key browser to rescan")
		}
		return a.setToast("use :import to reload") // file mode fallback
	case ActJsonView:
		if p != nil && p.Mode == ModeSheet {
			p.JsonViewOff = 0
			p.Mode = ModeJsonView
		}
		return nil
	case ActToggleSelect:
		if p != nil && f != nil && nrows > 0 {
			p.Table.ToggleSelect(p.Table.Sel())
		}
		return nil
	case ActDelete:
		return a.startDelete()
	case ActNextPage:
		if a.backend != nil {
			if oobe, ok := a.backend.(db.Paged); ok {
				oobe.SetPage(oobe.GetPage() + 1)
				return a.runCommand("refreshtable", "")
			}
			// SQL tables: load the next window (keyset when the table has a
			// single sort key, OFFSET otherwise) instead of re-fetching.
			if bb, ok := a.backend.(db.BrowseBackend); ok {
				if st := a.activeBrowse(); st != nil && !st.done {
					req := &db.BrowsePageRequest{
						After: st.after,
						Page:  st.page + 1,
						Limit: TableFetchLimit,
					}
					st := st
					return func() tea.Msg {
						// The ctx lives inside the Cmd closure: a defer in dispatch
						// fires the moment dispatch returns the command — before the
						// runtime runs it — and BrowsePage received an already-cancelled
						// context (green tests hid this: the fake ignores ctx).
						ctx, cancel := context.WithTimeout(context.Background(), browsePageTimeout)
						defer cancel()
						res, err := bb.BrowsePage(ctx, st.ns, st.table, req)
						return BrowsePageMsg{st: st, res: res, err: err}
					}
				}
				// The pane is not a browsable table (a query result, a panel):
				// there is no page walk to be "at the end of", so stay silent
				// like ActPrevPage's equivalent — the old toast claimed "last
				// page" for a pane that never paged.
				if st := a.activeBrowse(); st != nil && st.done {
					return a.setToast("已是最后一页")
				}
				return nil
			}
		}
		return nil
	case ActPrevPage:
		if a.backend != nil {
			if oobe, ok := a.backend.(db.Paged); ok {
				if oobe.GetPage() > 0 {
					oobe.SetPage(oobe.GetPage() - 1)
					return a.runCommand("refreshtable", "")
				}
				return a.setToast("已是第一页")
			}
			// SQL 表：已加载的窗口行都在 base frame 里，< 就地把光标上移
			// 一窗（与 > 的服务端续读对称）；已在顶时如实反馈，不再静默。
			if p := a.pane(); p != nil {
				before := p.Table.Sel()
				p.Table.Move(-TableFetchLimit, nrows)
				if p.Table.Sel() == before {
					return a.setToast("已在第一页")
				}
			}
			return nil
		}
		return nil

	case ActColFocus:
		if ncols == 0 {
			return nil
		}
		if p.ColZoomed {
			// Exit zoom: restore saved TableCols and cursor position.
			p.ColZoomed = false
			p.TableCols = p.savedTableCols
			p.savedTableCols = nil
			p.HScroll = 0
			// Restore column cursor to where it was before zoom.
			targetCols := len(p.TableCols)
			if targetCols == 0 {
				targetCols = ncols
			}
			p.Table.SetColCursor(p.savedColCursor, targetCols)
		} else {
			// Zoom into the column under the cursor.
			cur := p.Table.ColCursor()
			originalIdx := cur
			if len(p.TableCols) > 0 && cur < len(p.TableCols) {
				originalIdx = p.TableCols[cur]
			}
			p.savedTableCols = p.TableCols
			p.savedColCursor = cur // save sub-frame cursor position
			p.ColZoomed = true
			p.TableCols = []int{originalIdx}
			p.HScroll = 0
		}
		return nil

	case ActOobeCols:
		if a.backend != nil && a.backend.Kind() == "openobserve" {
			return a.runCommand("oobecols", "")
		}
		return nil

	// mode switches
	case ActSheet:
		if nrows > 0 {
			p.Mode = ModeSheet
			p.SheetOff = 0
			p.SheetField = 0
			p.SheetValOff = 0
			p.SheetEditing = false
			p.SheetEdit = LineInput{}
			p.SheetFilter = LineInput{}
			p.SheetFiltering = false
			// Warm the column-metadata cache for this table so the sheet shows
			// the real engine type (e.g. "varchar(255)") instead of the frame
			// DType fallback ("str" on mysql). Skipped when the cache is already
			// populated or in file mode (no backend -> warmColumnMeta is nil).
			if a.backend != nil && a.columnMetaFor(p.Title) == nil {
				return a.warmColumnMeta(p.Title)
			}
		}
	case ActSheetFilter:
		// "/" enters filter input with a fresh buffer. While already filtering
		// this is never reached: handleKey routes to handleSheetFilterKey
		// before dispatch, so a literal "/" lands as a printable rune.
		p.SheetFilter = LineInput{}
		p.SheetFiltering = true
		return nil
	case ActBack:
		p.Mode = ModeTable
	case ActSheetDown, ActSheetUp:
		matched := sheetMatchedCols(f, p.SheetFilter.String())
		if len(matched) == 0 {
			return nil
		}
		delta := 1
		if action == ActSheetUp {
			delta = -1
		}
		p.SheetField = clamp(p.SheetField+delta, 0, len(matched)-1)
		// A new field shows its value from the top, and cancels any in-flight
		// inline edit (the edit buffer belongs to the previous field).
		p.SheetValOff = 0
		if p.SheetEditing {
			p.SheetEditing = false
			p.SheetEdit = LineInput{}
		}
		a.edgeFollowSheet(f, p)
	case ActCopy:
		text := rowClipboardText(f, p.Table.Sel())
		return tea.Batch(tea.SetClipboard(text), a.setToast("copied to clipboard"))
	case ActCopyCell:
		if nrows == 0 || ncols == 0 {
			return nil
		}
		c := clamp(p.Table.ColCursor(), 0, ncols-1)
		r := clamp(p.Table.Sel(), 0, nrows-1)
		return tea.Batch(
			tea.SetClipboard(f.CellString(r, c)),
			a.setToast("copied cell "+f.Columns[c].Name),
		)
	case ActCopyRow:
		if nrows == 0 {
			return nil
		}
		r := clamp(p.Table.Sel(), 0, nrows-1)
		return tea.Batch(
			tea.SetClipboard(rowClipboardText(f, r)),
			a.setToast(fmt.Sprintf("copied row %d", r+1)),
		)

	// searches / commands
	case ActFuzzy:
		if len(p.TableCols) > 0 {
			p.HScroll = 0
			a.search.StartCols(f, true, p.TableCols)
		} else {
			a.search.Start(f, true)
		}
	case ActExact:
		a.search.Start(f, false)
	case ActInfo:
		return a.runCommand("info", "")
	case ActGoto:
		return a.runCommand("goto", key)

	// stack / tabs
	case ActPop:
		return a.pop()
	}
	return nil
}

// pop implements "q": sheet -> table, stack pop, close tab, quit on last.
func (a *App) pop() tea.Cmd {
	p := a.pane()
	if p == nil {
		return tea.Quit
	}
	if p.Mode == ModeSheet {
		p.Mode = ModeTable
		return nil
	}
	if p.Pop() {
		return nil
	}
	return a.closeTab(a.active)
}

// startDelete gathers the rows to delete (the multi-select set, or the cursor
// row when nothing is selected) and hands off to the confirm-delete popup. In
// database mode it first resolves the table's primary keys: a table without a
// primary key cannot be safely deleted and is rejected with a toast. File mode
// always has an implicit identity (row index) so it skips the PK check.
func (a *App) startDelete() tea.Cmd {
	p := a.pane()
	if p == nil {
		return nil
	}
	f := p.Current()
	if f == nil {
		return nil
	}
	nrows := f.NumRows()
	if nrows == 0 {
		return nil
	}
	rows := p.Table.SelectedRows()
	if len(rows) == 0 {
		rows = []int{clamp(p.Table.Sel(), 0, nrows-1)}
	}
	if a.backend != nil {
		table := a.BaseCrumb()
		ns := a.CurrentTableNamespace()
		// The PK probe is a catalog query (30s timeout): issuing it from the
		// Update loop froze the whole TUI for that long on a dead connection.
		// Resolve it in the command's goroutine; the ready message re-enters
		// the update loop to stage the confirm overlay.
		return func() tea.Msg {
			pks, err := a.backend.PrimaryKeys(ns, table)
			if err != nil {
				return ErrorMsg{Err: err}
			}
			if len(pks) == 0 {
				return ToastMsg{Text: "no primary key, cannot delete"}
			}
			return deleteKeysReadyMsg{frame: f, rows: rows, table: table, ns: ns}
		}
	}
	// file mode
	a.pendingDelete = &PendingDelete{Frame: f, Rows: rows}
	return a.runCommand("confirmdelete", "")
}

// escBack implements esc's "go back one level" in table mode. Unlike pop it
// never closes a tab and never quits: sheet -> table, stack pop, and on the
// base frame it steps back OUT to the browser overlay (db mode) or the
// connection form (empty db-mode workspace). In file mode the base frame is
// the bottom of the hierarchy and esc does nothing.
func (a *App) escBack() tea.Cmd {
	dbMode := a.backend != nil || a.kv != nil
	p := a.pane()
	if p == nil {
		// Empty workspace: back to the connection form (registered by the
		// db-mode integration as the "connect" factory).
		if dbMode {
			return a.runCommand("connect", "")
		}
		return nil
	}
	if p.Mode == ModeSheet {
		p.Mode = ModeTable
		return nil
	}
	if p.Pop() {
		return nil
	}
	// Base frame: one level up is the schema browser (SQL backends) or the
	// key browser (the db-mode integration wraps the "schema" factory to
	// serve it when a key-value connection is live).
	if dbMode {
		return a.runCommand("schema", "")
	}
	return nil
}

// --- commands ---------------------------------------------------------------------

// runCommand resolves a command name: reserved built-ins first, then the
// popup factory registry.
func (a *App) runCommand(name, arg string) tea.Cmd {
	switch name {
	case "quit":
		return tea.Quit
	case "reset":
		if p := a.pane(); p != nil {
			p.Reset()
		}
		return nil
	case "toggleborders":
		a.showBorders = !a.showBorders
		a.persistUI()
		return nil
	case "togglerownumbers":
		a.showRowNumbers = !a.showRowNumbers
		a.persistUI()
		return nil
	case "reloadconfig":
		uc, err := config.ReadUIConfig()
		if err != nil {
			a.overlays = append(a.overlays, errBox{err: fmt.Errorf("reload config: %w", err)})
			return nil
		}
		if th, ok := theme.Builtin(uc.Theme); ok {
			a.th, a.themeName = th, uc.Theme
		}
		a.showBorders = uc.ShowBorders
		a.showRowNumbers = uc.ShowRowNumbers
		return a.setToast("config reloaded")
	case "saveedit":
		if a.pendingEdit == nil {
			return nil
		}
		pe := *a.pendingEdit
		a.pendingEdit = nil
		if p := a.pane(); p != nil {
			p.SheetEditing = false
		}
		return commitCellEdit(a, pe)
	case "canceledit":
		a.pendingEdit = nil
		if p := a.pane(); p != nil {
			p.SheetEditing = false
		}
		return a.setToast("edit canceled")
	case "deleterows":
		if a.pendingDelete == nil {
			return nil
		}
		pd := *a.pendingDelete
		a.pendingDelete = nil
		if p := a.pane(); p != nil {
			p.Table.ClearSelect()
		}
		return commitRowDelete(a, pd)
	case "canceldelete":
		a.pendingDelete = nil
		if p := a.pane(); p != nil {
			p.Table.ClearSelect()
		}
		return nil
	}

	factory, ok := Factories[name]
	if !ok {
		// The search bar is part of the app itself; serve these directly
		// when no popup has claimed the names.
		switch name {
		case "search", "fuzzysearch":
			if f := a.CurrentFrame(); f != nil {
				a.search.Start(f, name == "fuzzysearch")
			}
			return nil
		}
		a.overlays = append(a.overlays, errBox{err: fmt.Errorf("unknown command: %s", name)})
		return nil
	}
	ov, err := factory(a, arg)
	if err != nil {
		a.overlays = append(a.overlays, errBox{err: err})
		return nil
	}
	if ov != nil {
		return a.pushOverlay(ov)
	}
	return nil
}

// pushOverlay appends an overlay to the stack and starts its Init command
// when it has one (OverlayIniter).
func (a *App) pushOverlay(ov Overlay) tea.Cmd {
	a.overlays = append(a.overlays, ov)
	if in, ok := ov.(OverlayIniter); ok {
		return in.Init()
	}
	return nil
}

// PushOverlay adds an overlay before the program starts. Database mode uses
// it to show the connection form on top of the empty workspace.
func (a *App) PushOverlay(ov Overlay) {
	if ov != nil {
		a.overlays = append(a.overlays, ov)
	}
}

// applyFrame handles ApplyFrameMsg: push onto the originating pane's stack or
// open a new tab.
func (a *App) applyFrame(m ApplyFrameMsg) tea.Cmd {
	if m.Frame == nil {
		return nil
	}
	if m.RegisterAs != "" && a.engine != nil {
		_ = a.engine.Register(m.RegisterAs, m.Frame) // best effort, mirrors CLI load
	}
	newTab := m.NewTab || len(a.tabs) == 0
	var target *Pane
	if !newTab {
		if m.PaneID != 0 {
			// Route the result back to the pane the command was run from; the
			// user may have switched tabs while it executed. A vanished pane
			// (tab closed mid-flight) falls back to opening a new tab.
			target = a.paneByID(m.PaneID)
			newTab = target == nil
		} else {
			target = a.pane()
			newTab = target == nil
		}
	}
	if newTab {
		title := m.TabTitle
		if title == "" {
			title = m.Crumb
		}
		if title == "" {
			title = "result"
		}
		p := NewPane(title, m.Frame)
		p.TableCols = m.TableCols
		p.sourceNS, p.sourceTable = m.SourceNamespace, m.SourceTable
		a.tabs = append(a.tabs, p)
		a.active = len(a.tabs) - 1
		a.seedBrowse(p)
	} else {
		crumb := m.Crumb
		if crumb == "" {
			crumb = "op"
		}
		target.Push(m.Frame, crumb)
	}
	if m.Truncated {
		// The frame holds only the first AdHocRowLimit rows; say so, so the
		// result never reads as complete (an export or a trusted view would
		// silently drop the tail).
		return a.setToast(fmt.Sprintf("showing first %d rows (ad-hoc cap reached)", dbcore.AdHocRowLimit))
	}
	return nil
}

// seedBrowse records where a table tab's window stands. The refresh path
// (ReplaceBaseMsg) re-seeds so a mid-walk refresh starts over at page 1.
func (a *App) seedBrowse(p *Pane) {
	if p.sourceNS == "" || p.sourceTable == "" {
		return
	}
	key := p.sourceNS + "|" + p.sourceTable
	base := p.Base()
	if base == nil {
		return
	}
	state := &browseState{
		ns:     p.sourceNS,
		table:  p.sourceTable,
		paneID: p.ID(),
		page:   1,
		loaded: base.NumRows(),
	}
	a.browse[key] = state
}

// activeBrowse is the browse state of the active tab's table, or nil when the
// active tab is not a table or has no window yet.
func (a *App) activeBrowse() *browseState {
	p := a.pane()
	if p == nil || p.sourceNS == "" || p.sourceTable == "" {
		return nil
	}
	return a.browse[p.sourceNS+"|"+p.sourceTable]
}

// BrowsePageMsg carries one "next page" window back to the pane it was
// requested for.
type BrowsePageMsg struct {
	st  *browseState
	res *db.BrowsePageResult
	err error
}

// browsePageApplied lands a BrowsePageMsg: append the window to the pane's
// frame, update the cursor and the window bounds, and report the page.
func (a *App) browsePageApplied(m BrowsePageMsg) tea.Cmd {
	if m.err != nil {
		return a.setToast("加载下一页失败：" + m.err.Error())
	}
	st, res := m.st, m.res
	p := a.paneByID(st.paneID)
	if p == nil || res == nil || res.Frame == nil {
		return nil // the tab closed while the window was in flight
	}
	base := p.Base()
	if base == nil {
		return nil
	}
	window := res.Frame
	for i := 0; i < window.NumRows(); i++ {
		base.AppendRow(window.Row(i))
	}
	// The cursor continues the window: the sort column's value at the end
	// (keyset), or the OFFSET page (no sort key).
	if window.NumRows() == 0 {
		// An empty window means the walk is exhausted: mark done so the
		// next ">" reports "已是最后一页" instead of fetching again.
		st.done = true
		return a.setToast("已是最后一页")
	}
	if res.SortColumn != "" {
		col := window.ColumnIndex(res.SortColumn)
		last := window.Row(window.NumRows() - 1)
		if col >= 0 {
			switch v := last[col].(type) {
			case nil:
				// keyset cannot continue past a NULL sort key: WHERE pk > NULL
				// matches nothing, so the walk is exhausted, not stuck.
				st.done = true
			case time.Time:
				// ISO 8601 with T: the text cursor binds against a datetime
				// sort column, and fmt.Sprint's "+0800 CST" suffix parses
				// nowhere.
				st.after = []string{v.Format("2006-01-02T15:04:05.9999999")}
			case []byte:
				// A BLOB sort column continues as its text form: fmt.Sprint's
				// "[104 101 ...]" byte list never matches in SQL.
				st.after = []string{string(v)}
			default:
				st.after = []string{fmt.Sprint(v)}
			}
			st.sortColumn = res.SortColumn
		}
	}
	// The page counter feeds the toast: it advances in keyset mode too.
	st.page++
	st.loaded += window.NumRows()
	if res.TotalFound {
		st.total, st.totalFound = res.Total, true
	}
	st.done = window.NumRows() < TableFetchLimit

	status := fmt.Sprintf("第 %d 页 · 已加载 %d 行", st.page, st.loaded)
	if st.totalFound {
		status += fmt.Sprintf(" · 共 %d 行", st.total)
	}
	if st.done {
		status += "（已到最后一页）"
	}
	return a.setToast(status)
}

// replaceBase handles ReplaceBaseMsg: swap the base frame of the target pane
// (used by the refresh command to reload the current table in place).
func (a *App) replaceBase(m ReplaceBaseMsg) tea.Cmd {
	var target *Pane
	if m.PaneID != 0 {
		target = a.paneByID(m.PaneID)
	}
	if target == nil {
		target = a.pane()
	}
	if target == nil || m.Frame == nil {
		return nil
	}
	target.ReplaceBase(m.Frame)
	// Re-seed the browse walk: the fresh frame is page 1 again, so the
	// walk restarts from the top instead of continuing on a stale cursor.
	a.seedBrowse(target)
	return nil
}

// paneByID finds an open pane by its stable ID; nil when it was closed.
func (a *App) paneByID(id int) *Pane {
	for _, p := range a.tabs {
		if p.ID() == id {
			return p
		}
	}
	return nil
}

func (a *App) closeTab(i int) tea.Cmd {
	if len(a.tabs) == 0 {
		return tea.Quit
	}
	i = clamp(i, 0, len(a.tabs)-1)
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	if len(a.tabs) == 0 {
		return tea.Quit
	}
	// Closing a tab before the active one shifts the slice left; follow the
	// active pane so the user keeps looking at the same tab.
	if i < a.active {
		a.active--
	}
	if a.active >= len(a.tabs) {
		a.active = len(a.tabs) - 1
	}
	return nil
}

func (a *App) registerTable(name string) tea.Cmd {
	if a.engine == nil {
		a.overlays = append(a.overlays, errBox{err: fmt.Errorf("no embedded SQL engine in this mode")})
		return nil
	}
	f := a.CurrentFrame()
	if f == nil {
		a.overlays = append(a.overlays, errBox{err: fmt.Errorf("no frame to register")})
		return nil
	}
	if err := a.engine.Register(name, f); err != nil {
		a.overlays = append(a.overlays, errBox{err: err})
		return nil
	}
	return a.setToast("registered as " + name)
}

// ensureSynced re-registers the active frame under "_" so that
// `select * from _` targets what the user is looking at. The registration is
// O(rows), so it runs lazily — only when SQL is about to execute (Engine())
// and only if the frame under view changed since the last sync (frames are
// immutable by convention, so pointer identity is a valid cache key).
func (a *App) ensureSynced() {
	if a.engine == nil {
		return
	}
	if f := a.CurrentFrame(); f != nil && f != a.syncedFrame {
		if a.engine.Register("_", f) == nil { // best effort
			a.syncedFrame = f
		}
	}
}

func (a *App) setToast(text string) tea.Cmd {
	a.toastText = text
	a.toastID++
	id := a.toastID
	return tea.Tick(toastDuration, func(time.Time) tea.Msg {
		return toastExpiredMsg{id: id}
	})
}

func (a *App) persistUI() {
	_ = config.WriteUIConfig(&config.UIConfig{ // best effort
		Theme:          a.themeName,
		ShowBorders:    a.showBorders,
		ShowRowNumbers: a.showRowNumbers,
	})
}

func (a *App) pane() *Pane {
	if len(a.tabs) == 0 {
		return nil
	}
	if a.active >= len(a.tabs) {
		a.active = len(a.tabs) - 1
	}
	return a.tabs[a.active]
}

// --- AppContext ----------------------------------------------------------------

func (a *App) CurrentFrame() *data.Frame {
	if p := a.pane(); p != nil {
		return p.Current()
	}
	return nil
}

func (a *App) CurrentRow() int {
	if p := a.pane(); p != nil {
		return p.Table.Sel()
	}
	return 0
}

// CurrentTableNamespace reports the namespace the active table was loaded
// from, so the refresh command can re-fetch the same table. It consults the
// completion cache's table-to-namespace map first, then falls back to the
// backend's connected namespace (when the backend exposes one), and finally
// returns "" (the backend then treats it as the default namespace).
func (a *App) CurrentTableNamespace() string {
	if a.backend == nil {
		return ""
	}
	if p := a.pane(); p != nil {
		c := a.completionCache()
		c.mu.Lock()
		ns := c.ns[p.Title]
		c.mu.Unlock()
		if ns != "" {
			return ns
		}
	}
	if cn, ok := a.backend.(db.Namespaced); ok {
		return cn.CurrentNamespace()
	}
	return ""
}

// warmColumnMeta returns a tea.Cmd that fetches the real column metadata for
// table off the update loop. The resulting columnMetaMsg populates the
// completion cache, so the next View shows the engine-native type inline with
// the value instead of the frame DType fallback. The cache is touched under
// lock only when the connection still matches by the time the fetch returns.
// Returns nil in file mode (no backend).
func (a *App) warmColumnMeta(table string) tea.Cmd {
	be := a.backend
	if be == nil || table == "" {
		return nil
	}
	ns := a.CurrentTableNamespace()
	tableC := table
	return func() tea.Msg {
		m, err := be.ColumnsMeta(ns, tableC)
		return columnMetaMsg{table: tableC, meta: m, err: err}
	}
}

// cursorFieldType returns just the data-type token of the active sheet field
// (e.g. "int", "varchar(255)", "i64"). It reuses cursorFieldMeta so the same
// cache-then-DType fallback path applies: real engine type when the cache is
// warm, the frame DType otherwise. Empty when no pane/frame is active.
func (a *App) cursorFieldType() string {
	dt, _, _, _ := a.cursorFieldMeta()
	return dt
}

func (a *App) BaseCrumb() string {
	if p := a.pane(); p != nil {
		return p.Title
	}
	return ""
}

// TableSource reports the live table the active pane's base frame was
// fetched from (namespace may be empty for engines without namespaces; the
// table is empty when the pane did not originate from a table fetch).
func (a *App) TableSource() (namespace, table string) {
	if p := a.pane(); p != nil {
		return p.sourceNS, p.sourceTable
	}
	return "", ""
}

func (a *App) Crumbs() []string {
	if p := a.pane(); p != nil {
		return p.Crumbs()
	}
	return nil
}

func (a *App) ColumnNames() []string {
	if f := a.CurrentFrame(); f != nil {
		return f.ColumnNames()
	}
	return nil
}

func (a *App) Engine() *query.Engine {
	a.ensureSynced()
	return a.engine
}

func (a *App) TableNames() []string {
	var names []string
	if a.engine != nil {
		if ts, err := a.engine.Tables(); err == nil {
			names = append(names, ts...)
		}
	}
	if a.backend != nil {
		if nss, err := a.backend.Namespaces(); err == nil {
			for _, ns := range nss {
				if ts, err := a.backend.Tables(ns); err == nil {
					names = append(names, ts...)
				}
			}
		}
	}
	return names
}

func (a *App) Backend() db.Backend { return a.backend }
func (a *App) KV() db.KVBackend    { return a.kv }

func (a *App) Theme() *theme.Theme { return a.th }
func (a *App) ThemeName() string   { return a.themeName }

func (a *App) ShowBorders() bool    { return a.showBorders }
func (a *App) ShowRowNumbers() bool { return a.showRowNumbers }

func (a *App) Tabs() []TabInfo {
	out := make([]TabInfo, len(a.tabs))
	for i, p := range a.tabs {
		shape := "0 x 0"
		if f := p.Current(); f != nil {
			shape = fmt.Sprintf("%d x %d", f.NumRows(), f.NumCols())
		}
		out[i] = TabInfo{Title: p.Title, Shape: shape}
	}
	return out
}

func (a *App) ActiveTab() int { return a.active }

func (a *App) ActivePaneID() int {
	if p := a.pane(); p != nil {
		return p.ID()
	}
	return 0
}

// PendingEdit reports the in-flight cell edit awaiting commit, or nil when
// no edit is in progress. Confirm popups read it to render the proposed
// change; the "saveedit" command consumes it.
func (a *App) PendingEdit() *PendingEdit { return a.pendingEdit }

// PendingDelete reports the in-flight row delete awaiting commit, or nil
// when no delete is in progress.
func (a *App) PendingDelete() *PendingDelete { return a.pendingDelete }

func (a *App) searchQuery() string {
	if a.search.Active() {
		return a.search.Query()
	}
	return ""
}

// compile-time interface checks
var (
	_ tea.Model  = (*App)(nil)
	_ AppContext = (*App)(nil)
)
