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
		viewLen: 20,
	}
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

// dashFolder holds state for one folder in the Dashboards group.
type dashFolder struct {
	folderID   string
	name       string
	open       bool
	loading    bool
	loaded     bool
	dashboards []oobe.DashboardInfo
	err        string
}

type oobeSchemaOverlay struct {
	ctx     ui.AppContext
	backend *oobe.Backend

	open [4]bool

	// Logs
	streamsLoading bool
	streamsLoaded  bool
	streams        []string
	streamsErr     string

	// Dashboards (two-level: folder → dashboard)
	foldersLoading bool
	foldersLoaded  bool
	folders        []dashFolder
	foldersErr     string

	cursor  int
	offset  int
	viewLen int
}

// ─── async messages ────────────────────────────────────────────────────────────

type oobeStreamsMsg struct {
	owner   *oobeSchemaOverlay
	streams []string
	err     error
}

type oobeFoldersMsg struct {
	owner   *oobeSchemaOverlay
	folders []dashFolder
	err     error
}

type oobeFolderDashboardsMsg struct {
	owner      *oobeSchemaOverlay
	folderID   string
	dashboards []oobe.DashboardInfo
	err        error
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func (o *oobeSchemaOverlay) Init() tea.Cmd {
	return tea.Batch(o.loadStreams(), o.loadFolders())
}

func (o *oobeSchemaOverlay) loadStreams() tea.Cmd {
	if o.streamsLoading || o.streamsLoaded {
		return nil
	}
	o.streamsLoading = true
	be := o.backend
	org := be.CurrentNamespace()
	owner := o
	return func() tea.Msg {
		streams, err := be.Tables(org)
		return oobeStreamsMsg{owner: owner, streams: streams, err: err}
	}
}

func (o *oobeSchemaOverlay) loadFolders() tea.Cmd {
	if o.foldersLoading || o.foldersLoaded {
		return nil
	}
	o.foldersLoading = true
	be := o.backend
	owner := o
	return func() tea.Msg {
		infos, err := be.ListFolders()
		if err != nil {
			return oobeFoldersMsg{owner: owner, err: err}
		}
		folders := make([]dashFolder, len(infos))
		for i, f := range infos {
			folders[i] = dashFolder{folderID: f.FolderID, name: f.Name}
		}
		return oobeFoldersMsg{owner: owner, folders: folders}
	}
}

func (o *oobeSchemaOverlay) loadFolderDashboards(i int) tea.Cmd {
	if i < 0 || i >= len(o.folders) {
		return nil
	}
	f := &o.folders[i]
	if f.loading || f.loaded {
		return nil
	}
	f.loading = true
	be := o.backend
	folderID := f.folderID
	folderName := f.name
	owner := o
	return func() tea.Msg {
		dashes, err := be.ListDashboardsInFolder(folderID, folderName)
		return oobeFolderDashboardsMsg{owner: owner, folderID: folderID, dashboards: dashes, err: err}
	}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (o *oobeSchemaOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	switch m := msg.(type) {
	case oobeStreamsMsg:
		if m.owner != o {
			return o, nil
		}
		o.streamsLoading = false
		o.streamsLoaded = true
		if m.err != nil {
			o.streamsErr = m.err.Error()
		} else {
			o.streams = m.streams
			o.streamsErr = ""
		}
		return o, nil

	case oobeFoldersMsg:
		if m.owner != o {
			return o, nil
		}
		o.foldersLoading = false
		o.foldersLoaded = true
		if m.err != nil {
			o.foldersErr = m.err.Error()
		} else {
			o.folders = m.folders
			o.foldersErr = ""
		}
		return o, nil

	case oobeFolderDashboardsMsg:
		if m.owner != o {
			return o, nil
		}
		for i := range o.folders {
			if o.folders[i].folderID == m.folderID {
				o.folders[i].loading = false
				o.folders[i].loaded = true
				if m.err != nil {
					o.folders[i].err = m.err.Error()
				} else {
					o.folders[i].dashboards = m.dashboards
					o.folders[i].err = ""
				}
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
		o.cursor = oobePrevSelectable(rows, o.cursor)
		o.edgeFollow(rows)
	case "down", "j":
		o.cursor = oobeNextSelectable(rows, o.cursor)
		o.edgeFollow(rows)
	case "r":
		var cmds []tea.Cmd
		if o.open[groupLogs] {
			o.streamsLoaded = false
			o.streamsLoading = false
			o.streamsErr = ""
			cmds = append(cmds, o.loadStreams())
		}
		if o.open[groupDashboards] {
			o.foldersLoaded = false
			o.foldersLoading = false
			o.foldersErr = ""
			for i := range o.folders {
				o.folders[i].loaded = false
				o.folders[i].loading = false
				o.folders[i].err = ""
				o.folders[i].dashboards = nil
			}
			cmds = append(cmds, o.loadFolders())
		}
		return o, tea.Batch(cmds...)
	case "enter", " ":
		if o.cursor < 0 || o.cursor >= len(rows) {
			return o, nil
		}
		r := rows[o.cursor]
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
			f := &o.folders[r.folderIdx]
			f.open = !f.open
			if f.open {
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
			if o.streamsLoading {
				rows = append(rows, oobeRow{kind: oobeRowNote, label: "  (loading…)"})
			} else if o.streamsErr != "" {
				rows = append(rows, oobeRow{kind: oobeRowNote, label: "  error: " + o.streamsErr})
			} else {
				for _, s := range o.streams {
					rows = append(rows, oobeRow{kind: oobeRowStream, label: "  " + s, streamName: s})
				}
				if len(o.streams) == 0 {
					rows = append(rows, oobeRow{kind: oobeRowNote, label: "  (no streams)"})
				}
			}
		case groupMetrics, groupTraces:
			rows = append(rows, oobeRow{kind: oobeRowNote, label: "  (coming soon)"})
		case groupDashboards:
			if o.foldersLoading {
				rows = append(rows, oobeRow{kind: oobeRowNote, label: "  (loading…)"})
			} else if o.foldersErr != "" {
				rows = append(rows, oobeRow{kind: oobeRowNote, label: "  error: " + o.foldersErr})
			} else {
				for i, f := range o.folders {
					fa := "  ▸ "
					if f.open {
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
					if !f.open {
						continue
					}
					if f.loading {
						rows = append(rows, oobeRow{kind: oobeRowNote, label: "    (loading…)"})
					} else if f.err != "" {
						rows = append(rows, oobeRow{kind: oobeRowNote, label: "    error: " + f.err})
					} else {
						for _, d := range f.dashboards {
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
						if len(f.dashboards) == 0 {
							rows = append(rows, oobeRow{kind: oobeRowNote, label: "    (no dashboards)"})
						}
					}
				}
				if len(o.folders) == 0 {
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
	o.viewLen = height - 4
	if o.viewLen < 1 {
		o.viewLen = 1
	}
	o.edgeFollow(rows)

	const bw = 60
	inner := bw - 2

	var lines []string

	// Connection title.
	connTitle := o.backend.Title()
	lines = append(lines, th.Subtle.Render(oobePad("  "+connTitle, inner)))
	lines = append(lines, th.Subtle.Render(strings.Repeat("─", inner)))

	// Visible rows.
	end := o.offset + o.viewLen
	if end > len(rows) {
		end = len(rows)
	}
	for i := o.offset; i < end; i++ {
		r := rows[i]
		line := oobePad(r.label, inner)
		if i == o.cursor && r.selectable() {
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
	lines = append(lines, th.Subtle.Render(oobePad(" enter open  •  r refresh  •  esc back", inner)))

	body := strings.Join(lines, "\n")
	box := ui.Box("schema", body, bw, th)
	return ui.FillPage(box, width, height)
}

func (o *oobeSchemaOverlay) Fullscreen() bool { return true }

// ─── helpers ──────────────────────────────────────────────────────────────────

func (o *oobeSchemaOverlay) edgeFollow(rows []oobeRow) {
	if o.cursor < 0 {
		o.cursor = 0
	}
	if o.cursor >= len(rows) {
		o.cursor = len(rows) - 1
	}
	if o.cursor < o.offset {
		o.offset = o.cursor
	}
	if o.cursor >= o.offset+o.viewLen {
		o.offset = o.cursor - o.viewLen + 1
	}
	if o.offset < 0 {
		o.offset = 0
	}
}

func oobeNextSelectable(rows []oobeRow, cur int) int {
	for i := cur + 1; i < len(rows); i++ {
		if rows[i].selectable() {
			return i
		}
	}
	return cur
}

func oobePrevSelectable(rows []oobeRow, cur int) int {
	for i := cur - 1; i >= 0; i-- {
		if rows[i].selectable() {
			return i
		}
	}
	return cur
}

func oobePad(s string, w int) string {
	for len(s) < w {
		s += " "
	}
	if len(s) > w {
		s = s[:w]
	}
	return s
}

// formatTimeRange is defined in oobedashboard.go (shared within popup package).
