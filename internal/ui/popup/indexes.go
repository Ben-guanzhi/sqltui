package popup

// `:indexes <table>`——索引浏览与删除（MySQL Workbench 索引页的 TUI 形态）。
// 内核 ListIndexes 列出、ddl.DropIndex 渲染；Enter 二步确认后把 DROP INDEX
// 塞进 query 命令（走编辑器——**确认流天然存在**：用户看到语句再运行，
// 与 :snip 的载入同路）。

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/ddl"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	RegisterCommand(Command{
		Name:        "indexes",
		Aliases:     []string{"idx"},
		Description: "browse/drop table indexes (Enter twice to drop)",
	}, func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		be := ctx.Backend()
		if be == nil {
			return nil, fmt.Errorf("索引浏览需要数据库连接")
		}
		table := strings.TrimSpace(arg)
		if table == "" {
			return nil, fmt.Errorf("用法 :indexes <表名>")
		}
		d, ok := be.(interface{ Driver() dbcore.SQLDriver })
		if !ok || d.Driver() == nil {
			return nil, fmt.Errorf("当前驱动不暴露内核 catalog——无法浏览索引")
		}
		return newIndexesOverlay(d, table), nil
	})
}

// indexesOverlay 列出一张表的索引，Enter 两步确认 Drop。
type indexesOverlay struct {
	be          interface{ Driver() dbcore.SQLDriver }
	table       string
	items       []dbcore.IndexInfo
	chosen      int
	killPending string // 两步确认：第一次 Enter 记下名字，第二次同一名才生成
	err         string
	loaded      bool
}

func newIndexesOverlay(be interface{ Driver() dbcore.SQLDriver }, table string) *indexesOverlay {
	o := &indexesOverlay{be: be, table: table}
	o.reload()
	return o
}

func (o *indexesOverlay) reload() {
	d := o.be.Driver()
	if d == nil {
		o.err = "当前驱动不暴露内核 catalog"
		o.loaded = true
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	idxs, err := d.ListIndexes(ctx, "", o.table)
	if err != nil {
		o.err = err.Error()
		o.loaded = true
		return
	}
	// 主键不是索引（表级约束），过滤。
	out := idxs[:0]
	for _, ix := range idxs {
		if !ix.Primary {
			out = append(out, ix)
		}
	}
	o.items = out
	o.loaded = true
}

func (o *indexesOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch key.String() {
	case "up", "k":
		if o.chosen > 0 {
			o.chosen--
		}
	case "down", "j":
		if o.chosen < len(o.items)-1 {
			o.chosen++
		}
	case "r":
		o.reload()
	case "enter":
		if o.chosen >= len(o.items) {
			return o, nil
		}
		name := o.items[o.chosen].Name
		// 两步确认（:procs 的同一手法）——DROP INDEX 不可逆。
		if o.killPending != name {
			o.killPending = name
			return o, nil
		}
		o.killPending = ""
		// 方言拼写走内核 DropIndex；Kind 是 dbcore 的引擎标签。
		stmt := ddl.DropIndex(string(o.be.Driver().Kind()), "", o.table, name)
		return o, tea.Sequence(ui.CloseOverlay, func() tea.Msg {
			return ui.RunCommandMsg{Name: "query", Arg: stmt}
		})
	case "esc", "ctrl+c", "q":
		return o, ui.CloseOverlay
	}
	return o, nil
}

func (o *indexesOverlay) View(width, height int, th *theme.Theme) string {
	boxW := 72
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	b.WriteString(" " + th.Header.Render("Indexes · "+o.table) + "\n\n")
	if !o.loaded {
		b.WriteString(" " + th.Subtle.Render("读取中…") + "\n")
		return ui.Box(" :indexes ", b.String(), boxW, th)
	}
	if o.err != "" {
		b.WriteString(" " + th.Error.Render(o.err) + "\n")
		return ui.Box(" :indexes ", b.String(), boxW, th)
	}
	if len(o.items) == 0 {
		b.WriteString(" " + th.Subtle.Render("这张表没有非主键索引") + "\n")
	}
	for i, ix := range o.items {
		cursor := "  "
		if i == o.chosen {
			cursor = "> "
		}
		kind := "INDEX"
		if ix.Unique {
			kind = "UNIQUE"
		}
		line := fmt.Sprintf("%s%s [%s] (%s)", cursor, ix.Name, kind, strings.Join(ix.Columns, ", "))
		if i == o.chosen {
			b.WriteString(" " + th.RowSelected.Render(line) + "\n")
		} else {
			b.WriteString(" " + th.Text.Render(line) + "\n")
		}
		if i == o.chosen && o.killPending == ix.Name {
			b.WriteString("   " + th.Warning.Render("再按 Enter 生成 DROP INDEX（进编辑器运行）") + "\n")
		}
	}
	b.WriteString("\n " + th.Subtle.Render("↑↓ 选择 · r 刷新 · Enter×2 生成 DROP · esc 关") + "\n")
	return ui.Box(" :indexes ", b.String(), boxW, th)
}
