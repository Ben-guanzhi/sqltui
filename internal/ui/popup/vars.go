package popup

// `:vars`——服务器系统变量（Navicat「变量」/ HeidiSQL Variables 面板的 TUI
// 形态）。**只读**：内核 ServerVariablesReader 没有改值（SET GLOBAL 的副作用
// 与权限语义各家不同）。↑↓ 滚动、输入即过滤（名字/值）、r 刷新、Esc 关。
// 引擎不支持时 overlay 给一行说明。

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
		Name:        "vars",
		Aliases:     []string{"variables"},
		Description: "show server system variables (read-only)",
	}, func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		return newVarsList(strings.TrimSpace(arg)), nil
	})
}

// varsList 是变量列表 overlay：过滤词 + 滚动位置。
type varsList struct {
	items   []dbcore.ServerVariable
	errText string
	filter  string
	top     int // 视口顶（过滤后列表的行号）
}

func newVarsList(preFilter string) *varsList {
	v := &varsList{filter: preFilter}
	v.reload()
	return v
}

func (v *varsList) reload() {
	vars, err := ui.ListServerVariables()
	if err != nil {
		v.errText = err.Error()
		v.items = nil
		return
	}
	v.errText = ""
	v.items = vars
	v.top = 0
}

// filtered 过滤后的列表（名字或值包含过滤词，大小写不敏感）。
func (v *varsList) filtered() []dbcore.ServerVariable {
	term := strings.ToLower(v.filter)
	if term == "" {
		return v.items
	}
	out := make([]dbcore.ServerVariable, 0, len(v.items)/4+1)
	for _, sv := range v.items {
		if strings.Contains(strings.ToLower(sv.Name), term) ||
			strings.Contains(strings.ToLower(sv.Value), term) {
			out = append(out, sv)
		}
	}
	return out
}

func (v *varsList) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return v, nil
	}
	visible := v.filtered()
	switch key.String() {
	case "esc", "ctrl+c":
		return v, ui.CloseOverlay
	case "r", "F5":
		v.reload()
		return v, nil
	case "up", "k":
		if v.top > 0 {
			v.top--
		}
	case "down", "j", "enter":
		// enter/下键：向下滚一屏内一行。
		if v.top < len(visible)-1 {
			v.top++
		}
	case "backspace":
		if v.filter != "" {
			r := []rune(v.filter)
			v.filter = string(r[:len(r)-1])
			v.top = 0
		}
	default:
		// 可打印字符追加进过滤词（输入即过滤）。
		s := key.String()
		if len([]rune(s)) == 1 && s[0] >= 0x20 {
			v.filter += s
			v.top = 0
		}
	}
	_ = visible
	return v, nil
}

func (v *varsList) View(width, height int, th *theme.Theme) string {
	boxW := 84
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	rows := v.filtered()
	var b strings.Builder
	if v.errText != "" {
		b.WriteString(" " + th.Subtle.Render(v.errText) + "\n")
	} else {
		b.WriteString(" " + th.Subtle.Render(fmt.Sprintf(
			"变量 %d/%d（只读）· 输入即过滤 · ↑↓ 滚动 · r 刷新", len(rows), len(v.items))) + "\n")
		b.WriteString(" " + th.Subtle.Render("过滤: "+v.filter) + "\n")
		if len(rows) == 0 {
			b.WriteString(" " + th.Subtle.Render("没有匹配的变量。") + "\n")
		} else {
			// 视口：top 起，盒内约 height-4 行。
			viewRows := height - 6
			if viewRows < 3 {
				viewRows = 3
			}
			end := v.top + viewRows
			if end > len(rows) {
				end = len(rows)
			}
			for i := v.top; i < end; i++ {
				sv := rows[i]
				row := fmt.Sprintf("  %-28s %s", sv.Name, sv.Value)
				r := []rune(row)
				if len(r) > boxW-4 {
					row = string(r[:boxW-4]) + "…"
				}
				// 改了未生效（value != default）的标 *。
				if sv.Default != "" && sv.Value != sv.Default {
					row += " *"
				}
				b.WriteString(" " + row + "\n")
			}
			if end < len(rows) {
				b.WriteString(" " + th.Subtle.Render(fmt.Sprintf("… 还有 %d 行（↓ 继续）", len(rows)-end)) + "\n")
			}
		}
	}
	b.WriteString(" " + th.Subtle.Render("Esc 关闭 · * = 与默认值不同 · 只读（不改值）") + "\n")
	return ui.Box("Server Variables", b.String(), boxW, th)
}
