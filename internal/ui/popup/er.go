package popup

// ER 图（TUI 版）：把 schema 的表与外键画成**文字**的依赖图。
//
// TUI 画不了盒子与折线，但「哪张表指向哪张表」用缩进与箭头一样读得懂——
// 被引用者（父）在上、引用者在下，每条外键一行 `子.列 → 父.列`。
//
// 布局沿用 dbstudio 的分层思路（被引用者排在前面），但**列的顺序**在这里
// 就是阅读顺序：先按「被引用的次数」排（被引用最多的最上），同次数按字母。

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

// erRow 是一行 ER 输出：表名，或它的一条外键。
type erRow struct {
	text  string
	kind  int // 0 = 表标题, 1 = 外键
	depth int
}

// erDiagram 是 ER 浮层：拿 schema 的外键 + 表名，输出文字图。
type erDiagram struct {
	rows []erRow
	err  string
}

// newErDiagram 从后端取外键并排版。后端不支持时把话说明白。
func newErDiagram(ctx ui.AppContext, namespace string) *erDiagram {
	be, ok := ctx.Backend().(db.ErBackend)
	if !ok {
		return &erDiagram{err: "此后端不支持外键内省，画不了 ER 图"}
	}
	fks, err := be.ForeignKeys(namespace)
	if err != nil {
		return &erDiagram{err: fmt.Sprintf("读外键失败：%v", err)}
	}
	return &erDiagram{rows: layoutErRows(fks)}
}

// layoutErRows 是纯排版：给外键集，出「被引用最多者在最上」的行序列。
// 取数与排版分开，测试只钉排版。
func layoutErRows(fks []db.ForeignKey) []erRow {
	tables := map[string]bool{}
	for _, f := range fks {
		tables[f.FromTable] = true
		tables[f.ToTable] = true
	}
	if len(tables) == 0 {
		return nil
	}

	// 被引用次数：决定谁排在上面（被引用最多的 = 最上游）。
	refCount := map[string]int{}
	for _, f := range fks {
		refCount[f.ToTable]++
	}
	names := make([]string, 0, len(tables))
	for t := range tables {
		names = append(names, t)
	}
	sort.Slice(names, func(i, j int) bool {
		if refCount[names[i]] != refCount[names[j]] {
			return refCount[names[i]] > refCount[names[j]]
		}
		return names[i] < names[j]
	})

	// 每张表：标题 + 它的外键（按列名排序，确定性）。
	rows := make([]erRow, 0, len(names)+len(fks))
	for _, t := range names {
		rows = append(rows, erRow{text: t, kind: 0})
		own := make([]db.ForeignKey, 0)
		for _, f := range fks {
			if f.FromTable == t {
				own = append(own, f)
			}
		}
		sort.Slice(own, func(i, j int) bool {
			if own[i].FromColumn != own[j].FromColumn {
				return own[i].FromColumn < own[j].FromColumn
			}
			return own[i].ToTable < own[j].ToTable
		})
		for _, f := range own {
			rows = append(rows, erRow{
				text:  fmt.Sprintf("%s.%s → %s.%s", f.FromTable, f.FromColumn, f.ToTable, f.ToColumn),
				kind:  1,
				depth: 1,
			})
		}
	}
	return rows
}

func init() {
	ui.Factories["er"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		// 命令参数就是 namespace（`:er dbo`）；空则后端按当前库查。
		return newErDiagram(ctx, arg), nil
	}
}

func (d *erDiagram) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "q", "ctrl+c":
			return d, ui.CloseOverlay
		}
	}
	return d, nil
}

func (d *erDiagram) View(width, height int, th *theme.Theme) string {
	boxW := 76
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	b.WriteString(" " + th.Subtle.Render("ER 图（被引用者在最上）") + "\n")
	if d.err != "" {
		b.WriteString(" " + th.Error.Render(d.err) + "\n")
	} else {
		for _, r := range d.rows {
			if r.kind == 0 {
				b.WriteString(" " + th.Text.Render(r.text) + "\n")
			} else {
				b.WriteString("   " + th.Subtle.Render(r.text) + "\n")
			}
		}
	}
	b.WriteString(" " + th.Subtle.Render(" esc / q 关闭"))
	return ui.Box("er-diagram", b.String(), boxW, th)
}
