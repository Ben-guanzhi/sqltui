package popup

// 快捷键速查（对齐 dbstudio 的 Keyboard Shortcuts / TableProGo 的 ShortcutHelp）。
//
// sqltui 的键位分散在各处（app.go 的命令、form/redis 的局部键），底部
// footer 只给一行提示——**没有一个地方能看到全部**。这个 overlay 把命令与
// 常用键列在一屏里，用 `:help` 打开（Esc / ? 关闭）。
//
// 表是**数据**（不是渲染时拼字符串）：加键就加一行，速查不会漏。

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

// keyHelpRow 是一行键位说明。
type keyHelpRow struct {
	keys string
	what string
}

var keyHelpRows = []keyHelpRow{
	// ---- 导航 ----
	{"j / ↓", "下移"},
	{"k / ↑", "上移"},
	{"h / ←", "返回上一层（schema / key 浏览器）"},
	{"l / →", "进入"},
	{"enter", "确认 / 执行"},
	{"tab / shift+tab", "在字段或窗格之间移动"},
	{"esc", "返回 / 关闭"},
	// ---- 编辑 ----
	{"ctrl+s", "保存改动"},
	{"ctrl+u", "清空输入"},
	{"ctrl+c", "取消 / 关闭"},
	{"backspace", "删除"},
	// ---- 命令 ----
	{":help", "打开/关闭本速查"},
	{":fav", "收藏 / 取消收藏最近一条语句"},
	{":favs", "看收藏列表"},
	{":snip <名字>", "把最近一条语句存为命名片段"},
	{":snips", "片段列表（Enter 载入 :query）"},
	{":procs", "服务器会话列表（Enter 杀，两步确认）"},
	{":vars", "服务器系统变量（只读，输入即过滤）"},
	{":dict", "导出数据字典到 Markdown（0600）"},
	{":indexes <表名>", "索引浏览，Enter×2 生成 DROP INDEX"},
	{":gendata <表名>", "生成测试数据 INSERT 进编辑器"},
	{":quit", "退出"},
	{":reset", "重置当前窗格"},
	{":toggleborders", "切换边框显示"},
	{":togglerownumbers", "切换行号"},
	{":reloadconfig", "重新读取配置"},
	{":saveedit / :canceledit", "保存 / 放弃编辑"},
	{":deleterows", "删除行（进入确认）"},
	{":connect / :schema / :query", "数据库模式下切换视图"},
}

// keyHelp 是速查 overlay：任何键都吞掉（不传给下层），esc/? 关闭。
type keyHelp struct{}

// NewKeyHelp 构造速查 overlay（:help 与测试用）。
func NewKeyHelp() *keyHelp { return &keyHelp{} }

// init 把 `help` 注册进命令工厂：`ui` 包不该 import `popup`（会成环），
// 所以由 popup 自己登记，runCommand 的内置 switch 落不到它时就走工厂。
func init() {
	ui.Factories["help"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		return NewKeyHelp(), nil
	}
}

func (k *keyHelp) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "?", "ctrl+c":
			return k, ui.CloseOverlay
		}
	}
	return k, nil
}

func (k *keyHelp) View(width, height int, th *theme.Theme) string {
	boxW := 56
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}

	var b strings.Builder
	b.WriteString(" " + th.Subtle.Render("键盘快捷键") + "\n")
	for _, r := range keyHelpRows {
		key := ansiTruncate(r.keys, 22)
		b.WriteString(" " + th.Text.Render(padRight(key, 22)) + " " + r.what + "\n")
	}
	b.WriteString(" " + th.Subtle.Render(" :help 或 ? 关闭"))
	return ui.Box("keyhelp", b.String(), boxW, th)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func ansiTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
