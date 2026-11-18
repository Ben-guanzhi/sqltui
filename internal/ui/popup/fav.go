package popup

// `:fav`——切换最近一条语句的收藏标记。
//
// 历史记下**所有**跑过的；收藏是标出来要反复用的。TUI 的最简形态：没有
// 面板，就是一条命令 + toast 回显（★ 已收藏 / ☆ 已取消）。要看收藏列表时
// 用 palette（历史条目带 ★ 前缀由后续接入）。

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	ui.Factories["fav"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		stmt, fav, ok := ui.ToggleLastFavorite()
		if !ok {
			return &favToast{text: "还没有执行过任何语句"}, nil
		}
		head := stmt
		if i := strings.IndexByte(head, '\n'); i >= 0 {
			head = head[:i]
		}
		if r := []rune(head); len(r) > 60 {
			head = string(r[:57]) + "…"
		}
		mark := "☆ 已取消收藏"
		if fav {
			mark = "★ 已收藏"
		}
		return &favToast{text: fmt.Sprintf("%s：%s", mark, head)}, nil
	}
}

// favToast 是一个只回显一行的 overlay：任何键关闭。
type favToast struct{ text string }

func (f *favToast) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "enter", "q", "ctrl+c":
			return f, ui.CloseOverlay
		}
	}
	return f, nil
}

func (f *favToast) View(width, height int, th *theme.Theme) string {
	boxW := 72
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	return ui.Box("fav", " "+th.Text.Render(f.text), boxW, th)
}
