package popup

// Confirm-risk overlay（§344）：destructive 语句在查询编辑器回车时不直接跑，
// 先弹出内核 sqlrisk 的分级摘要（影响半径 + 首条原因），y/enter 确认执行、
// n/esc 取消——DataGrip 的"危险语句确认"在 TUI 的形态，与 confirmdelete/
// confirmsave 的 y 确认手法一致。

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Ben-guanzhi/dbcore/sqlrisk"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	ui.Factories["confirmrisk"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		stmt := strings.TrimSpace(arg)
		if stmt == "" {
			return nil, errConfirmRiskNoStatement
		}
		return newConfirmRiskOverlay(ctx, stmt, nil), nil
	}
	addConfirmRiskCommand()
}

var errConfirmRiskNoStatement = overlayError("confirmrisk: 需要语句参数")

// overlayError 是 Factory 返回 err 时宿主显示的错误类型。
type overlayError string

func (e overlayError) Error() string { return string(e) }

func addConfirmRiskCommand() {
	registerPaletteOnly(Command{Name: "confirmrisk", Description: "confirm a destructive statement (y/esc)"})
}

// confirmRiskOverlay 展示 sqlrisk 摘要；确认后发出 riskRun 消息（携带语句），
// 取消则关闭。语句通过 riskRun 消息交回调用方执行。
type confirmRiskOverlay struct {
	ctx  ui.AppContext
	stmt string
	risk sqlrisk.Risk
}

func newConfirmRiskOverlay(ctx ui.AppContext, stmt string, _ any) *confirmRiskOverlay {
	return &confirmRiskOverlay{ctx: ctx, stmt: stmt, risk: sqlrisk.Assess(stmt)}
}

func (o *confirmRiskOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch key.String() {
	case "y", "enter":
		// 交回 query 命令管道（与 :snip 载入同路）：编辑器载入语句并运行。
		return o, tea.Sequence(ui.CloseOverlay, func() tea.Msg {
			return ui.RunCommandMsg{Name: "query", Arg: o.stmt}
		})
	case "n", "esc", "ctrl+c", "q":
		return o, ui.CloseOverlay
	}
	return o, nil
}

func (o *confirmRiskOverlay) View(width, height int, th *theme.Theme) string {
	boxW := 72
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	level := string(o.risk.Level)
	b.WriteString(" " + th.Warning.Render("["+level+"] "+o.risk.Summary) + "\n")
	for _, r := range o.risk.Reasons {
		b.WriteString(" " + th.Text.Render("· "+r) + "\n")
	}
	inner := boxW - 4
	b.WriteString("\n " + th.Text.Render("执行这条语句？") + "\n")
	b.WriteString(" " + th.Subtle.Render(confirmTrunc(o.stmt, inner)) + "\n")
	b.WriteString("\n " + th.Subtle.Render(" y 确认执行  •  n/esc 取消"))
	return ui.Box(" 风险确认 ", b.String(), boxW, th)
}

var _ ui.Overlay = (*confirmRiskOverlay)(nil)

// fmt 保持引用（摘要在 View 里可能扩展为多行截断）。
var _ = fmt.Sprintf
