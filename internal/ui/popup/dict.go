package popup

// `:dict`——数据字典导出（DBeaver Generate Documentation 的 TUI 形态）。
// 内核 ExportDictionary（dbcore v0.13.0）经 backend 暴露的 SQLDriver 生成
// Markdown，Enter 写入当前目录（0600），Esc 取消。
//
// TUI 没有系统保存对话框——用默认文件名 + 可编辑路径输入（:~/ 展开），与
// `:export` 同构。backend 不支持内核驱动时给明确说明（不静默失败）。

import (
	"context"
	"fmt"
	"time"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	RegisterCommand(Command{
		Name:        "dict",
		Aliases:     []string{"dictionary"},
		Description: "export data dictionary to Markdown",
	}, func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		be := ctx.Backend()
		if be == nil {
			return nil, fmt.Errorf("数据字典需要数据库连接（文件模式无 catalog）")
		}
		d, ok := be.(interface{ Driver() dbcore.SQLDriver })
		if !ok || d.Driver() == nil {
			return nil, fmt.Errorf("当前驱动不暴露内核 catalog——无法导出数据字典")
		}
		path := strings.TrimSpace(arg)
		if path == "" {
			path = fmt.Sprintf("data-dictionary-%s.md", timeStamp())
		} else {
			path = expandDictPath(path)
		}
		return &dictOverlay{
			ctx:  ctx,
			drv:  d.Driver(),
			ns:   ctx.CurrentTableNamespace(),
			path: path,
		}, nil
	})
}

// dictOverlay 的编辑态：path 输入框 + 状态。
type dictOverlay struct {
	ctx    ui.AppContext
	drv    dbcore.SQLDriver
	ns     string
	path   string
	err string
}

func expandDictPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func (o *dictOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	if pm, ok := msg.(tea.PasteMsg); ok {
		o.path += ui.SanitizePaste(pm.Content)
		return o, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch key.String() {
	case "enter":
		text, err := func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return dbcore.ExportDictionary(ctx, o.drv, o.ns, nil)
		}()
		if err != nil {
			o.err = err.Error()
			return o, nil
		}
		if werr := os.WriteFile(o.path, []byte(text), 0o600); werr != nil {
			o.err = "写文件失败：" + werr.Error()
			return o, nil
		}
		return o, tea.Sequence(ui.CloseOverlay, func() tea.Msg {
			return ui.ToastMsg{Text: fmt.Sprintf("数据字典已写入 %s", o.path)}
		})
	case "backspace":
		if len(o.path) > 0 {
			o.path = o.path[:len(o.path)-1]
		}
	case "esc", "ctrl+c":
		return o, ui.CloseOverlay
	default:
		// 单字符输入（路径）——过滤控制字符。
		s := key.String()
		if len(s) == 1 && s[0] >= 0x20 && s[0] != 0x7f {
			o.path += s
		}
	}
	return o, nil
}

func (o *dictOverlay) View(width, height int, th *theme.Theme) string {
	boxW := 64
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	b.WriteString(" " + th.Header.Render("数据字典导出") + "\n\n")
	b.WriteString(" 输出路径：" + th.Text.Render(o.path) + "\n")
	b.WriteString(" " + th.Subtle.Render("Enter 生成并写入 · Esc 取消 · 直接输入改路径") + "\n")
	if o.err != "" {
		b.WriteString(" " + th.Error.Render(o.err) + "\n")
	}
	return ui.Box(" :dict ", b.String(), boxW, th)
}
