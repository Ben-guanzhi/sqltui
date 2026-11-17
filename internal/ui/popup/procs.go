package popup

// `:procs`——服务器会话列表（Navicat「会话管理」的 TUI 形态）。
//
// ↑↓ 选择，Enter 终止选中（**两步确认**：第一次高亮并提示，第二次同一行
// Enter 才执行——杀会话让对方未提交事务回滚，不一键直发）；r 刷新；Esc 关。
// 数据走 internal/ui/procs.go 的内核 ProcessLister 桥（引擎不支持时工厂直接
// 给一行说明）。

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	RegisterCommand(Command{
		Name:        "procs",
		Aliases:     []string{"sessions"},
		Description: "list/kill server sessions (MySQL/PG/MSSQL)",
	}, func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		return newProcsList(), nil
	})
}

// procsList 是会话列表 overlay。
type procsList struct {
	items       []dbcore.ServerProcess
	chosen      int
	errText     string
	killPending string // 两步确认：第一次 Enter 记下 ID，第二次同一 ID 才执行
}

func newProcsList() *procsList {
	p := &procsList{}
	p.reload()
	return p
}

// reload 拉一次会话列表（同步——overlay 打开时列表就要在）。
func (p *procsList) reload() {
	list, err := ui.ListServerProcesses()
	if err != nil {
		p.errText = err.Error()
		p.items = nil
		return
	}
	p.errText = ""
	p.items = list
	p.chosen = 0
	p.killPending = ""
}

func (p *procsList) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	switch key.String() {
	case "esc", "q", "ctrl+c":
		return p, ui.CloseOverlay
	case "r", "F5":
		p.reload()
		return p, nil
	case "up", "k":
		if p.chosen > 0 {
			p.chosen--
		}
		// 换行取消待确认的 kill。
		p.killPending = ""
	case "down", "j":
		if p.chosen < len(p.items)-1 {
			p.chosen++
		}
		p.killPending = ""
	case "enter", "x":
		if len(p.items) == 0 || p.chosen >= len(p.items) {
			return p, nil
		}
		id := p.items[p.chosen].ID
		if p.killPending != id {
			// 第一步：记下待确认的 ID，提示再按一次。
			p.killPending = id
			return p, nil
		}
		// 第二步：真正终止。
		err := ui.KillServerProcess(id)
		p.killPending = ""
		if err != nil {
			p.errText = "终止失败：" + err.Error()
			return p, nil
		}
		p.reload()
		return p, func() tea.Msg { return ui.ToastMsg{Text: "已终止 " + id} }
	}
	return p, nil
}

func (p *procsList) View(width, height int, th *theme.Theme) string {
	boxW := 88
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	if p.errText != "" {
		b.WriteString(" " + th.Subtle.Render(p.errText) + "\n")
	} else if len(p.items) == 0 {
		b.WriteString(" " + th.Subtle.Render("服务器上没有其它活动会话。") + "\n")
	} else {
		b.WriteString(" " + th.Subtle.Render(fmt.Sprintf("会话 %d 个（不含当前连接）· ↑↓ 选择 · Enter 杀（两步确认） · r 刷新", len(p.items))) + "\n")
		for i, pr := range p.items {
			row := fmt.Sprintf("  %-8s %-18s %-12s %-10s %s",
				pr.ID, pr.User, pr.State, pr.Time+"s", procQueryHead(pr.Query))
			r := []rune(row)
			if len(r) > boxW-4 {
				row = string(r[:boxW-4]) + "…"
			}
			switch {
			case i == p.chosen && p.killPending == pr.ID:
				b.WriteString(" " + th.Error.Render(row+"  ← 再按 Enter 确认终止") + "\n")
			case i == p.chosen:
				b.WriteString(" " + th.RowSelected.Render(row) + "\n")
			default:
				b.WriteString(" " + row + "\n")
			}
		}
	}
	b.WriteString(" " + th.Subtle.Render("Esc 关闭 · 杀会话会让对方未提交事务回滚") + "\n")
	return ui.Box("Sessions", b.String(), boxW, th)
}

// procQueryHead 语句首行截断。
func procQueryHead(q string) string {
	first := strings.TrimSpace(strings.SplitN(q, "\n", 2)[0])
	r := []rune(first)
	if len(r) > 30 {
		first = string(r[:30]) + "…"
	}
	return first
}
