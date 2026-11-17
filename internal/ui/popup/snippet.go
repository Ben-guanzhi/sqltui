package popup

// `:snip <名字>`——把当前编辑器内容存为片段；`:snips`——片段列表，选中载入。
//
// 片段是**用户命名的、跨连接复用的 SQL 块**（Navicat 片段 / DataGrip live
// templates 的 TUI 形态）；收藏是历史条目的标记，片段可以先存没跑过的 SQL。
// 数据层在 internal/ui/snippet.go（与 dbstudio / fynequery 同一语义）。
//
// 载入 = 关闭列表并打开 `:query <SQL>`（sqltui 的编辑器是命令驱动的 popup，
// arg 即初始语句）——不设 ctx 编辑器回调，复用既有命令路径。

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	// `:snip <名字>`：保存当前编辑器内容为片段（同名覆盖）。
	// 当前语句取自 RunCommandMsg 的 arg（palette 把 `:query` 的 arg 传给
	// 编辑器）——这里同样经 arg 接收：`:snip 名字 :: <SQL>` 太绕，改为取
	// 「最近一条历史的语句」（刚跑过的就是编辑器里的常用场景）。
	ui.Factories["snip"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		name := strings.TrimSpace(arg)
		if name == "" {
			return &favToast{text: "用法 :snip <名字>——把最近一条语句存为片段"}, nil
		}
		hist := ui.LoadHistory()
		if len(hist) == 0 {
			return &favToast{text: "还没有执行过任何语句——先跑一条 SQL 再 :snip"}, nil
		}
		if err := ui.PutSnippet(name, hist[0].Stmt); err != nil {
			return &favToast{text: err.Error()}, nil
		}
		return &favToast{text: fmt.Sprintf("已存片段 %s：%s", name, ui.SnippetFirstLine(hist[0].Stmt))}, nil
	}

	RegisterCommand(Command{
		Name:        "snips",
		Aliases:     []string{"snip"},
		Description: "save/load named SQL snippets",
	}, func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		// `:snips` 列表；带 arg 时等价 `:snip <arg>`。
		if strings.TrimSpace(arg) != "" {
			hist := ui.LoadHistory()
			if len(hist) == 0 {
				return &favToast{text: "还没有执行过任何语句——先跑一条 SQL 再 :snips <名字>"}, nil
			}
			if err := ui.PutSnippet(strings.TrimSpace(arg), hist[0].Stmt); err != nil {
				return &favToast{text: err.Error()}, nil
			}
			return &favToast{text: fmt.Sprintf("已存片段 %s", strings.TrimSpace(arg))}, nil
		}
		return newSnipsList(), nil
	})
}

// newSnipsList 打开片段列表。
func newSnipsList() *snipsList {
	return &snipsList{items: ui.ListSnippets()}
}

// snipsList 是 `:snips` 的片段列表：↑↓ 选择，Enter 载入（打开 :query）。
type snipsList struct {
	items  []ui.Snippet
	chosen int // 高亮行
}

func (s *snipsList) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	switch key.String() {
	case "esc", "q", "ctrl+c":
		return s, ui.CloseOverlay
	case "up", "k":
		if s.chosen > 0 {
			s.chosen--
		}
	case "down", "j":
		if s.chosen < len(s.items)-1 {
			s.chosen++
		}
	case "enter", "d":
		if len(s.items) == 0 || s.chosen >= len(s.items) {
			return s, nil
		}
		sn := s.items[s.chosen]
		// 载入：关闭列表，打开带语句的查询编辑器（sqltui 的编辑器是
		// 命令驱动的 popup，arg 即初始语句）。
		return s, tea.Batch(
			ui.CloseOverlay,
			func() tea.Msg { return ui.RunCommandMsg{Name: "query", Arg: sn.SQL} },
		)
	}
	return s, nil
}

func (s *snipsList) View(width, height int, th *theme.Theme) string {
	boxW := 76
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	if len(s.items) == 0 {
		b.WriteString(" " + th.Subtle.Render("还没有片段——跑一条语句，:snip <名字> 存起来。") + "\n")
	} else {
		b.WriteString(" " + th.Subtle.Render(fmt.Sprintf("片段 %d 条（按名排序）· ↑↓ 选择 · Enter 载入", len(s.items))) + "\n")
		for i, sn := range s.items {
			row := fmt.Sprintf("  %s  %s", sn.Name, ui.SnippetFirstLine(sn.SQL))
			r := []rune(row)
			if len(r) > boxW-4 {
				row = string(r[:boxW-4]) + "…"
			}
			if i == s.chosen {
				b.WriteString(" " + th.RowSelected.Render(row) + "\n")
			} else {
				b.WriteString(" " + row + "\n")
			}
		}
	}
	b.WriteString(" " + th.Subtle.Render("Esc 关闭 · :snip <名字> 存最近一条语句") + "\n")
	return ui.Box("Snippets", b.String(), boxW, th)
}
