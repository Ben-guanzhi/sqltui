package popup

// `:favs`——收藏列表。收藏是「标出来要反复用的那几条」，看一眼、挑一条、
// 直接回编辑器。任何键关闭（列表本身不响应选择——挑取走 `:history` 的路，
// 这里先只读）。

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	ui.Factories["favs"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		return &favsList{items: ui.FavoriteHistory()}, nil
	}
}

type favsList struct {
	items []ui.HistoryRec
}

func (f *favsList) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "q", "ctrl+c":
			return f, ui.CloseOverlay
		}
	}
	return f, nil
}

func (f *favsList) View(width, height int, th *theme.Theme) string {
	boxW := 76
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	if len(f.items) == 0 {
		b.WriteString(" " + th.Subtle.Render("还没有收藏——跑一条语句，然后 :fav 标记它。") + "\n")
	} else {
		b.WriteString(" " + th.Subtle.Render(fmt.Sprintf("收藏 %d 条（最新在前）", len(f.items))) + "\n")
		for _, r := range f.items {
			head := r.Stmt
			if i := strings.IndexByte(head, '\n'); i >= 0 {
				head = head[:i]
			}
			// 超宽截断，行尾给省略号；连接名做前缀（跨连接的全局收藏要能分清）。
			line := head
			if r.Conn != "" {
				line = "[" + r.Conn + "] " + head
			}
			if w := width - 8; len(line) > w && w > 3 {
				line = line[:w-1] + "…"
			}
			b.WriteString(" ★ " + th.Text.Render(line) + "\n")
		}
	}
	b.WriteString(" " + th.Subtle.Render(" esc / q 关闭 · :fav 标记最近一条 · :favs 看这个列表"))
	return ui.Box("favs", b.String(), boxW, th)
}
