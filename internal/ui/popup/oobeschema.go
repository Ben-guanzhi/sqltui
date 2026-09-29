package popup

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/db/oobe"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

// NewOobeSchemaOverlay creates the oobe-specific 4-group schema browser.
// Registered as "schema" factory for openobserve by dbmode.RegisterFactories.
func NewOobeSchemaOverlay(ctx ui.AppContext) ui.Overlay {
	be, ok := ctx.Backend().(*oobe.Backend)
	if !ok {
		return nil
	}
	o := &oobeSchemaOverlay{
		ctx:     ctx,
		backend: be,
	}
	o.state.ViewLen = 20
	// all groups open by default
	o.open[groupLogs] = true
	o.open[groupMetrics] = true
	o.open[groupTraces] = true
	o.open[groupDashboards] = true
	return o
}

// oobeGroup indexes the 4 static section groups.
type oobeGroup int

const (
	groupLogs       oobeGroup = 0
	groupMetrics    oobeGroup = 1
	groupTraces     oobeGroup = 2
	groupDashboards oobeGroup = 3
)

var groupLabels = [4]string{"Logs", "Metrics", "Traces", "Dashboards"}

type oobeRowKind uint8

const (
	oobeRowGroup     oobeRowKind = iota // group header (selectable)
	oobeRowStream                       // log stream (selectable)
	oobeRowFolder                       // folder under Dashboards (selectable)
	oobeRowDashboard                    // dashboard under a folder (selectable)
	oobeRowNote                         // info/error/loading (not selectable)
)

type oobeRow struct {
	kind          oobeRowKind
	label         string
	group         oobeGroup
	streamName    string
	folderIdx     int
	dashboardInfo oobe.DashboardInfo
}

func (r oobeRow) selectable() bool {
	return r.kind == oobeRowGroup || r.kind == oobeRowStream ||
		r.kind == oobeRowFolder || r.kind == oobeRowDashboard
}

// dashFolder holds state for one folder in the Dashboards group. Its
// dashboards load lazily on first expansion.
type dashFolder struct {
	folderID string
	name     string
	ui.LazyGroup[oobe.DashboardInfo]
}

type oobeSchemaOverlay struct {
	ctx     ui.AppContext
	backend *oobe.Backend

	open [4]bool

	// Logs
	streams ui.LazyGroup[string]

	// Dashboards (two-level: folder → dashboard)
	folders ui.LazyGroup[dashFolder]

	state ui.ListState
}

// ─── async messages ────────────────────────────────────────────────────────────

type oobeStreamsMsg struct {
	ui.Owned[oobeSchemaOverlay]
	streams []string
	err     error
}

type oobeFoldersMsg struct {
	ui.Owned[oobeSchemaOverlay]
	folders []dashFolder
	err     error
}

type oobeFolderDashboardsMsg struct {
	ui.Owned[oobeSchemaOverlay]
	folderID   string
	dashboards []oobe.DashboardInfo
	err        error
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func (o *oobeSchemaOverlay) Init() tea.Cmd {
	return tea.Batch(o.loadStreams(), o.loadFolders())
}

func (o *oobeSchemaOverlay) loadStreams() tea.Cmd {
	be := o.backend
	org := be.CurrentNamespace()
	owner := o
	return o.streams.EnsureLoaded(func() tea.Cmd {
		return func() tea.Msg {
			streams, err := be.Tables(org)
			return oobeStreamsMsg{
				Owned:   ui.Owned[oobeSchemaOverlay]{Owner: owner},
				streams: streams, err: err,
			}
		}
	})
}

func (o *oobeSchemaOverlay) loadFolders() tea.Cmd {
	be := o.backend
	owner := o
	return o.folders.EnsureLoaded(func() tea.Cmd {
		return func() tea.Msg {
			infos, err := be.ListFolders()
			if err != nil {
				return oobeFoldersMsg{Owned: ui.Owned[oobeSchemaOverlay]{Owner: owner}, err: err}
			}
			folders := make([]dashFolder, len(infos))
			for i, f := range infos {
				folders[i] = dashFolder{folderID: f.FolderID, name: f.Name}
			}
			return oobeFoldersMsg{Owned: ui.Owned[oobeSchemaOverlay]{Owner: owner}, folders: folders}
		}
	})
}

func (o *oobeSchemaOverlay) loadFolderDashboards(i int) tea.Cmd {
	if i < 0 || i >= len(o.folders.Items) {
		return nil
	}
	f := &o.folders.Items[i]
	be := o.backend
	folderID := f.folderID
	folderName := f.name
	owner := o
	return f.EnsureLoaded(func() tea.Cmd {
		return func() tea.Msg {
			dashes, err := be.ListDashboardsInFolder(folderID, folderName)
			return oobeFolderDashboardsMsg{
				Owned:      ui.Owned[oobeSchemaOverlay]{Owner: owner},
				folderID:   folderID,
				dashboards: dashes, err: err,
			}
		}
	})
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (o *oobeSchemaOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	switch m := msg.(type) {
	case oobeStreamsMsg:
		if m.Stale(o) {
			return o, nil
		}
		o.streams.Deliver(m.streams, m.err)
		return o, nil

	case oobeFoldersMsg:
		if m.Stale(o) {
			return o, nil
		}
		o.folders.Deliver(m.folders, m.err)
		return o, nil

	case oobeFolderDashboardsMsg:
		if m.Stale(o) {
			return o, nil
		}
		for i := range o.folders.Items {
			if o.folders.Items[i].folderID == m.folderID {
				o.folders.Items[i].Deliver(m.dashboards, m.err)
				break
			}
		}
		return o, nil

	case tea.KeyPressMsg:
		return o.handleKey(m)
	}
	return o, nil
}

func (o *oobeSchemaOverlay) handleKey(msg tea.KeyPressMsg) (ui.Overlay, tea.Cmd) {
	rows := o.rows()
	key := msg.String()
	switch key {
	case "esc", "q":
		return o, func() tea.Msg { return ui.RunCommandMsg{Name: "connect"} }
	case "up", "k":
		o.state.Cursor = ui.NextSelectable(rows, o.state.Cursor, -1, oobeRow.selectable)
		o.edgeFollow(rows)
	case "down", "j":
		o.state.Cursor = ui.NextSelectable(rows, o.state.Cursor, +1, oobeRow.selectable)
		o.edgeFollow(rows)
	case "r":
		var cmds []tea.Cmd
		if o.open[groupLogs] {
			o.streams.Reset()
			cmds = append(cmds, o.loadStreams())
		}
		if o.open[groupDashboards] {
			o.folders.Reset()
			for i := range o.folders.Items {
				o.folders.Items[i].Reset()
			}
			cmds = append(cmds, o.loadFolders())
		}
		return o, tea.Batch(cmds...)
	case "enter", " ":
		if o.state.Cursor < 0 || o.state.Cursor >= len(rows) {
			return o, nil
		}
		r := rows[o.state.Cursor]
		switch r.kind {
		case oobeRowGroup:
			o.open[r.group] = !o.open[r.group]
			if o.open[r.group] {
				switch r.group {
				case groupLogs:
					return o, o.loadStreams()
				case groupDashboards:
					return o, o.loadFolders()
				}
			}
		case oobeRowFolder:
			f := &o.folders.Items[r.folderIdx]
			f.Open = !f.Open
			if f.Open {
				return o, o.loadFolderDashboards(r.folderIdx)
			}
		case oobeRowStream:
			org := o.backend.CurrentNamespace()
			arg := org + "\t" + r.streamName
			return o, tea.Sequence(
				ui.CloseOverlay,
				func() tea.Msg { return ui.RunCommandMsg{Name: "opentable", Arg: arg} },
			)
		case oobeRowDashboard:
			viewer := newPanelViewerOverlay(o.ctx, o.backend, r.dashboardInfo)
			return o, func() tea.Msg { return ui.PushOverlayMsg{Overlay: viewer} }
		}
	}
	return o, nil
}

// ─── rows ─────────────────────────────────────────────────────────────────────

func (o *oobeSchemaOverlay) rows() []oobeRow {
	var rows []oobeRow
	for g := oobeGroup(0); g < 4; g++ {
		arrow := "▸"
		if o.open[g] {
			arrow = "▾"
		}
		rows = append(rows, oobeRow{
			kind:  oobeRowGroup,
			label: arrow + " " + groupLabels[g],
			group: g,
		})
		if !o.open[g] {
			continue
		}
		switch g {
		case groupLogs:
			switch {
			case o.streams.Loading:
				rows = append(rows, oobeRow{kind: oobeRowNote, label: "  (loading…)"})
			case o.streams.Err != nil:
				rows = append(rows, oobeRow{kind: oobeRowNote, label: "  error: " + o.streams.Err.Error()})
			default:
				for _, s := range o.streams.Items {
					rows = append(rows, oobeRow{kind: oobeRowStream, label: "  " + s, streamName: s})
				}
				if len(o.streams.Items) == 0 {
					rows = append(rows, oobeRow{kind: oobeRowNote, label: "  (no streams)"})
				}
			}
		case groupMetrics, groupTraces:
			rows = append(rows, oobeRow{kind: oobeRowNote, label: "  (coming soon)"})
		case groupDashboards:
			switch {
			case o.folders.Loading:
				rows = append(rows, oobeRow{kind: oobeRowNote, label: "  (loading…)"})
			case o.folders.Err != nil:
				rows = append(rows, oobeRow{kind: oobeRowNote, label: "  error: " + o.folders.Err.Error()})
			default:
				for i := range o.folders.Items {
					f := &o.folders.Items[i]
					fa := "  ▸ "
					if f.Open {
						fa = "  ▾ "
					}
					flabel := f.name
					if flabel == "" {
						flabel = f.folderID
					}
					rows = append(rows, oobeRow{
						kind:      oobeRowFolder,
						label:     fa + flabel,
						folderIdx: i,
					})
					if !f.Open {
						continue
					}
					switch {
					case f.Loading:
						rows = append(rows, oobeRow{kind: oobeRowNote, label: "    (loading…)"})
					case f.Err != nil:
						rows = append(rows, oobeRow{kind: oobeRowNote, label: "    error: " + f.Err.Error()})
					default:
						for _, d := range f.Items {
							dlabel := d.Title
							if dlabel == "" {
								dlabel = d.DashboardID
							}
							rows = append(rows, oobeRow{
								kind:          oobeRowDashboard,
								label:         "    " + dlabel,
								folderIdx:     i,
								dashboardInfo: d,
							})
						}
						if len(f.Items) == 0 {
							rows = append(rows, oobeRow{kind: oobeRowNote, label: "    (no dashboards)"})
						}
					}
				}
				if len(o.folders.Items) == 0 {
					rows = append(rows, oobeRow{kind: oobeRowNote, label: "  (no folders)"})
				}
			}
		}
	}
	return rows
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (o *oobeSchemaOverlay) View(width, height int, th *theme.Theme) string {
	rows := o.rows()
	o.state.ViewLen = height - 4
	if o.state.ViewLen < 1 {
		o.state.ViewLen = 1
	}
	o.edgeFollow(rows)

	const bw = 60
	inner := bw - 2

	var lines []string

	// Connection title.
	connTitle := o.backend.Title()
	lines = append(lines, th.Subtle.Render(ui.Pad("  "+connTitle, inner)))
	lines = append(lines, th.Subtle.Render(strings.Repeat("─", inner)))

	// Visible rows.
	end := o.state.Offset + o.state.ViewLen
	if end > len(rows) {
		end = len(rows)
	}
	for i := o.state.Offset; i < end; i++ {
		r := rows[i]
		line := ui.Pad(r.label, inner)
		if i == o.state.Cursor && r.selectable() {
			lines = append(lines, th.ListSelected.Render(line))
		} else if r.kind == oobeRowGroup {
			lines = append(lines, th.Header.Render(line))
		} else if r.kind == oobeRowFolder {
			lines = append(lines, th.Header.Render(line))
		} else {
			lines = append(lines, th.ListItem.Render(line))
		}
	}

	// Hint line.
	lines = append(lines, th.Subtle.Render(strings.Repeat("─", inner)))
	lines = append(lines, th.Subtle.Render(ui.Pad(" enter open  •  r refresh  •  esc back", inner)))

	body := strings.Join(lines, "\n")
	box := ui.Box("schema", body, bw, th)
	return ui.FillPage(box, width, height)
}

func (o *oobeSchemaOverlay) Fullscreen() bool { return true }

// ─── helpers ──────────────────────────────────────────────────────────────────

// edgeFollow clamps the cursor onto the row list and scrolls just enough to
// keep it inside the visible window.
func (o *oobeSchemaOverlay) edgeFollow(rows []oobeRow) {
	if o.state.Cursor < 0 {
		o.state.Cursor = 0
	}
	if o.state.Cursor >= len(rows) {
		o.state.Cursor = len(rows) - 1
	}
	o.state.ScrollToCursor(len(rows), o.state.ViewLen)
}

// formatTimeRange is defined in oobedashboard.go (shared within popup package).
