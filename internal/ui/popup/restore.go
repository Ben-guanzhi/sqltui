package popup

// 恢复（restore）：把一个 .sql 文件的语句执行到当前连接。
//
// 恢复是**破坏性**的（备份产物自带 DROP，会换掉现有表），所以**两步**：
// 第一次 `:restore <路径>` 只显示**将执行的语句数**并要求确认；y/enter
// 才真正执行，n/esc 取消。与 confirmsave 的 y/enter 手法一致。
//
// 语句的切分见 splitRestoreSQL：**按「分号结尾的行」**——只用于本客户端
// 自己的备份产物（一行一条 INSERT），别拿它切用户脚本。

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

// restoreOverlay 是恢复确认：先预览（多少条语句），确认后执行。
type restoreOverlay struct {
	ctx   ui.AppContext
	path  string
	stmts []string
	err   string
}

func init() {
	ui.Factories["restore"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		path := strings.TrimSpace(arg)
		if path == "" {
			return nil, fmt.Errorf("用法 :restore <路径> —— 要导入的 .sql 文件")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读备份文件：%v", err)
		}
		stmts := splitRestoreSQL(string(raw))
		if len(stmts) == 0 {
			return nil, fmt.Errorf("文件里没有可执行的语句")
		}
		return &restoreOverlay{ctx: ctx, path: path, stmts: stmts}, nil
	}
}

// splitRestoreSQL 把 SQL 文本切成语句：按「分号结尾的行」。
// 与 sqlgpui 的 split_sql 同一边界——**只用于本客户端自己的备份产物**
// （一行一条 INSERT）；值里带分号的用户脚本会被切错。
func splitRestoreSQL(text string) []string {
	var out []string
	var cur strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		if strings.HasSuffix(t, ";") {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func (o *restoreOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch key.String() {
	case "y", "Y", "enter":
		// 确认：把全部语句串成一条命令链（跑完弹 toast 汇报）。
		be := o.ctx.Backend()
		var cmds []tea.Cmd
		for i, stmt := range o.stmts {
			s, n := stmt, i
			cmds = append(cmds, func() tea.Msg {
				if _, err := be.Run(s); err != nil {
					return ui.ToastMsg{Text: fmt.Sprintf("第 %d 条失败：%v", n+1, err)}
				}
				return nil
			})
		}
		cmds = append(cmds, func() tea.Msg {
			return ui.ToastMsg{Text: fmt.Sprintf("恢复完成（%d 条语句）", len(o.stmts))}
		})
		return o, tea.Sequence(ui.CloseOverlay, tea.Batch(cmds...))
	case "n", "N", "esc", "ctrl+c":
		return o, ui.CloseOverlay
	}
	return o, nil
}

func (o *restoreOverlay) View(width, height int, th *theme.Theme) string {
	boxW := 72
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	b.WriteString(" " + th.Text.Render("恢复备份") + "\n")
	b.WriteString(" " + th.Subtle.Render(o.path) + "\n")
	b.WriteString(" " + fmt.Sprintf("将执行 %d 条语句。", len(o.stmts)) + "\n")
	b.WriteString(" " + th.Warning.Render("这会替换现有表（备份产物自带 DROP）。") + "\n")
	b.WriteString(" " + th.Subtle.Render(" y 确认  ·  n 取消"))
	if o.err != "" {
		b.WriteString("\n " + th.Error.Render(o.err))
	}
	return ui.Box("restore", b.String(), boxW, th)
}
