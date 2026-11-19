package popup

// `:gendata <表>`——数据生成（DBeaver Mock Data 的 TUI 形态）。内核
// dbcore.GenerateInserts 按表列类型造数；语句通过 RunCommandMsg 进 query
// 编辑器——**确认流天然存在**：用户在编辑器里看到再运行（与 :snip/:indexes
// 同路）。

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	RegisterCommand(Command{
		Name:        "gendata",
		Description: "generate type-driven test INSERTs for a table",
	}, func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		be := ctx.Backend()
		if be == nil {
			return nil, fmt.Errorf("数据生成需要数据库连接")
		}
		d, ok := be.(interface{ Driver() dbcore.SQLDriver })
		if !ok || d.Driver() == nil {
			return nil, fmt.Errorf("当前驱动不暴露内核 catalog——无法读取列类型")
		}
		table := strings.TrimSpace(arg)
		if table == "" {
			return nil, fmt.Errorf("用法 :gendata <表名>")
		}
		drv := d.Driver()
		gctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cols, err := drv.ListColumns(gctx, "", table)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 的列失败: %w", table, err)
		}
		if len(cols) == 0 {
			return nil, fmt.Errorf("表 %s 没有已知列", table)
		}
		seed := uint64(time.Now().UnixNano())
		stmts := dbcore.GenerateInserts(cols, dbcore.GenOptions{
			Rows: 10, Seed: seed, Dialect: string(drv.Kind()),
			Table: table, SkipPrimaryKey: true,
		})
		if len(stmts) == 0 {
			return nil, fmt.Errorf("没有可生成的列")
		}
		return &genDataOverlay{
			table: table, statements: stmts, seed: seed,
		}, nil
	})
}

// genDataOverlay reports what was generated and asks Enter to put the SQL in
// the editor (never executes it — the user reviews the statements).
type genDataOverlay struct {
	table      string
	statements []string
	seed       uint64
}

func (o *genDataOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch key.String() {
	case "enter":
		return o, tea.Sequence(ui.CloseOverlay, func() tea.Msg {
			return ui.RunCommandMsg{Name: "query", Arg: strings.Join(o.statements, "\n")}
		})
	case "esc", "ctrl+c", "q":
		return o, ui.CloseOverlay
	}
	return o, nil
}

func (o *genDataOverlay) View(width, height int, th *theme.Theme) string {
	boxW := 68
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	b.WriteString(" " + th.Header.Render("Generate test data · "+o.table) + "\n\n")
	b.WriteString(fmt.Sprintf(" %d INSERT statements generated (seed %d).\n", len(o.statements), o.seed))
	b.WriteString(" " + th.Subtle.Render("Nothing ran — Enter puts the SQL in the editor for review.") + "\n\n")
	b.WriteString(" " + th.Text.Render(o.statements[0]) + "\n")
	if len(o.statements) > 1 {
		b.WriteString(" " + th.Subtle.Render(fmt.Sprintf("… and %d more", len(o.statements)-1)) + "\n")
	}
	b.WriteString("\n " + th.Subtle.Render("Enter 放入编辑器 · esc 关") + "\n")
	return ui.Box(" :gendata ", b.String(), boxW, th)
}
