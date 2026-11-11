package popup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/reader"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
	"github.com/LinPr/sqltui/internal/writer"
)

// editorRegistry tracks the editors editRunner launches so a UI exit can kill
// one that is still running and remove its temp file. It is the inverse of the
// launch: without it a quit (or a crash-recoverable return) orphans the editor
// and leaks the scratch CSV.
var (
	editorMu     sync.Mutex
	editorSeq    int
	editorCancel = map[int]context.CancelFunc{}
	editorTemps  = map[int]string{}
)

func trackEditor(cancel context.CancelFunc, tmp string) int {
	editorMu.Lock()
	editorSeq++
	id := editorSeq
	editorCancel[id] = cancel
	editorTemps[id] = tmp
	editorMu.Unlock()
	return id
}

func untrackEditor(id int) {
	editorMu.Lock()
	delete(editorCancel, id)
	delete(editorTemps, id)
	editorMu.Unlock()
}

// killRunningEditors cancels every tracked editor context (exec.CommandContext
// turns that into a process kill) and removes its temp file. Registered as a
// UI exit hook; idempotent.
func killRunningEditors() {
	editorMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(editorCancel))
	for _, c := range editorCancel {
		cancels = append(cancels, c)
	}
	tmps := make([]string, 0, len(editorTemps))
	for _, s := range editorTemps {
		tmps = append(tmps, s)
	}
	editorCancel = map[int]context.CancelFunc{}
	editorTemps = map[int]string{}
	editorMu.Unlock()

	for _, c := range cancels {
		c()
	}
	for _, s := range tmps {
		_ = os.Remove(s)
	}
}

func init() {
	ui.Factories["edit"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		frame := ctx.CurrentFrame()
		if frame == nil {
			return nil, errors.New("no data to edit")
		}

		tmp, err := os.CreateTemp("", "sqltui-edit-*.csv")
		if err != nil {
			return nil, fmt.Errorf("create temp file: %w", err)
		}
		cw, err := writer.For(writer.FormatCSV)
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return nil, err
		}
		werr := cw.Write(tmp, frame, writer.DefaultOptions())
		if cerr := tmp.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(tmp.Name())
			return nil, fmt.Errorf("write temp file: %w", werr)
		}

		// 编辑器进程用可取消的 CommandContext 启动：UI 退出钩子取消它即杀掉
		// 仍在运行的编辑器（逆操作），同时登记临时文件以便退出时清理。
		edCtx, edCancel := context.WithCancel(context.Background())
		return &editRunner{cmd: editEditorCmd(edCtx, tmp.Name()), tmp: tmp.Name(), paneID: ctx.ActivePaneID(), editorID: trackEditor(edCancel, tmp.Name())}, nil
	}
	// 退出钩子：杀掉仍在运行的编辑器并清理其临时文件。
	ui.RegisterExitHook(killRunningEditors)
}

// editEditorCmd builds the editor invocation for the temp file, honoring a
// multi-word $EDITOR ("code -w") and falling back to vi. The context cancels
// the launched process, which is how a UI exit kills a running editor.
func editEditorCmd(ctx context.Context, path string) *exec.Cmd {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	return exec.CommandContext(ctx, parts[0], append(parts[1:], path)...)
}

// editRunner is an invisible auto-run overlay: factories cannot dispatch
// commands, so it implements ui.OverlayIniter and emits the ExecProcessMsg
// (batched with its own close) the moment it is pushed. Update stays as a
// fallback fire path for a message that races in before the close.
type editRunner struct {
	cmd      *exec.Cmd
	tmp      string
	paneID   int
	editorID int
	started  bool
}

// Init fires the editor process as soon as the overlay is pushed.
func (e *editRunner) Init() tea.Cmd {
	if e.started {
		return nil
	}
	e.started = true
	tmp, paneID := e.tmp, e.paneID
	editorID := e.editorID
	exe := ui.ExecProcessMsg{
		Cmd: e.cmd,
		OnDone: func(err error) tea.Msg {
			// 逆操作：编辑器结束后撤销登记（正常路径）。退出钩子杀掉的情况
			// 这里只是幂等清理。
			untrackEditor(editorID)
			return editReload(tmp, paneID, err)
		},
	}
	return tea.Batch(
		ui.CloseOverlay,
		func() tea.Msg { return exe },
	)
}

func (e *editRunner) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	return e, e.Init()
}

func (e *editRunner) View(width, height int, th *theme.Theme) string { return "" }

var _ ui.OverlayIniter = (*editRunner)(nil)

// editReload re-reads the edited temp CSV and turns it into the follow-up
// app message: ApplyFrameMsg (routed to the originating pane) on success,
// ErrorMsg otherwise.
func editReload(tmp string, paneID int, procErr error) tea.Msg {
	defer os.Remove(tmp)
	if procErr != nil {
		return ui.ErrorMsg{Err: fmt.Errorf("editor failed: %w", procErr)}
	}
	r, err := reader.For(reader.FormatCSV)
	if err != nil {
		return ui.ErrorMsg{Err: err}
	}
	src, err := reader.FromFile(tmp)
	if err != nil {
		return ui.ErrorMsg{Err: fmt.Errorf("reload edited data: %w", err)}
	}
	opt := reader.DefaultOptions()
	opt.Format = reader.FormatCSV
	frames, err := r.Read(src, opt)
	if err != nil {
		return ui.ErrorMsg{Err: fmt.Errorf("reload edited data: %w", err)}
	}
	if len(frames) == 0 || frames[0].Frame == nil {
		return ui.ErrorMsg{Err: errors.New("edited file produced no data")}
	}
	return ui.ApplyFrameMsg{Frame: frames[0].Frame, Crumb: "edit", PaneID: paneID}
}
