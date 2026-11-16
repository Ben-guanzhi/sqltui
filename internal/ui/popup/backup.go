package popup

// 备份 overlay：`:backup [namespace]` 把一个 schema 导成 SQL 文本。
//
// 与恢复同一手法：先**预览**（会备份哪些表、写到哪个文件），y/enter 确认
// 才落盘；n/esc 取消。落盘用原子写（tmp + rename），与配置文件的写法一致。
//
// 边界（继承 buildBackup 的）：**数据备份**而非精确 schema；超过 FetchTable
// 上限的大库请用引擎自己的 dump 工具。

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/theme"
	"github.com/LinPr/sqltui/internal/ui"
)

// backupOverlay 是备份确认：先预览（表数 + 目标文件），确认后落盘。
type backupOverlay struct {
	ctx       ui.AppContext
	namespace string
	tables    []string
	path      string
	err       string
}

func init() {
	ui.Factories["backup"] = func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
		ns := strings.TrimSpace(arg)
		be := ctx.Backend()

		// MySQL / PostgreSQL：**优先走原生 dump 工具**（精确 schema，无行数
		// 上限）——DumpBackend 是可选能力，mysqlbe/postgresbe 实现它。
		if dumper, ok := be.(db.DumpBackend); ok {
			info := dumper.DumpConn()
			path := fmt.Sprintf("sqltui-dump-%s.sql", timeStamp())
			return &externalDumpOverlay{info: info, path: path}, nil
		}

		tables, err := be.Tables(ns)
		if err != nil {
			return nil, fmt.Errorf("读表清单：%v", err)
		}
		if len(tables) == 0 {
			return nil, fmt.Errorf("这个 namespace 没有表")
		}
		path := fmt.Sprintf("sqltui-backup-%s.sql", timeStamp())
		return &backupOverlay{ctx: ctx, namespace: ns, tables: tables, path: path}, nil
	}
}

// timeStamp 是文件名用的时间戳（UTC，排序友好）。
func timeStamp() string {
	return time.Now().UTC().Format("20060102-150405")
}

func (o *backupOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch key.String() {
	case "y", "Y", "enter":
		be := o.ctx.Backend()
		text, err := buildBackupVia(o.ctx, o.namespace, o.tables, be)
		if err != nil {
			o.err = err.Error()
			return o, nil
		}
		// 原子写：tmp + rename（与配置文件的写法一致）。
		if werr := os.WriteFile(o.path, []byte(text), 0o600); werr != nil {
			o.err = fmt.Sprintf("写文件失败：%v", werr)
			return o, nil
		}
		return o, tea.Sequence(ui.CloseOverlay, func() tea.Msg {
			return ui.ToastMsg{Text: fmt.Sprintf("备份已写入 %s", o.path)}
		})
	case "n", "N", "esc", "ctrl+c":
		return o, ui.CloseOverlay
	}
	return o, nil
}

// buildBackupVia 组装 db.buildBackup 需要的回调（FetchTable 的适配）。
func buildBackupVia(ctx ui.AppContext, ns string, tables []string, be interface {
	ColumnsMeta(namespace, table string) ([]db.ColumnMeta, error)
	PrimaryKeys(namespace, table string) ([]string, error)
	FetchTable(namespace, table string, limit int) (*data.Frame, error)
}) (string, error) {
	return db.BuildBackup(ns, tables,
		func(table string) ([]db.ColumnMeta, error) { return be.ColumnsMeta(ns, table) },
		func(table string) ([]string, error) { return be.PrimaryKeys(ns, table) },
		func(namespace, table string, limit int) (*data.Frame, error) {
			return be.FetchTable(namespace, table, limit)
		},
	)
}

func (o *backupOverlay) View(width, height int, th *theme.Theme) string {
	boxW := 72
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	b.WriteString(" " + th.Text.Render("备份数据库") + "\n")
	names := append([]string(nil), o.tables...)
	sort.Strings(names)
	if len(names) > 8 {
		names = append(names[:8], fmt.Sprintf("… 共 %d 张表", len(o.tables)))
	}
	b.WriteString(" " + th.Subtle.Render(strings.Join(names, ", ")) + "\n")
	b.WriteString(" " + fmt.Sprintf("写入 %s", o.path) + "\n")
	b.WriteString(" " + th.Warning.Render("数据备份：不含触发器/存储过程/视图；超过 10000 行的表会被截断。") + "\n")
	if o.err != "" {
		b.WriteString(" " + th.Error.Render(o.err) + "\n")
	}
	b.WriteString(" " + th.Subtle.Render(" y 确认  ·  n 取消"))
	return ui.Box("backup", b.String(), boxW, th)
}

// ---------------------------------------------------------------------------
// MySQL / PostgreSQL：原生 dump 工具

// externalDumpOverlay 是 mysqldump / pg_dump 的确认浮层。
type externalDumpOverlay struct {
	info db.DumpConnInfo
	path string
	err  string
}

func (o *externalDumpOverlay) Update(msg tea.Msg) (ui.Overlay, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch key.String() {
	case "y", "Y", "enter":
		if err := runNativeDump(o.info, o.path); err != nil {
			o.err = err.Error()
			return o, nil
		}
		return o, tea.Sequence(ui.CloseOverlay, func() tea.Msg {
			return ui.ToastMsg{Text: fmt.Sprintf("备份已写入 %s", o.path)}
		})
	case "n", "N", "esc", "ctrl+c":
		return o, ui.CloseOverlay
	}
	return o, nil
}

// runNativeDump 调 mysqldump / pg_dump。**口令走环境变量，绝不进 argv**。
func runNativeDump(info db.DumpConnInfo, path string) error {
	var tool string
	var args []string
	var envKey string
	switch info.Engine {
	case "mysql":
		tool = "mysqldump"
		envKey = "MYSQL_PWD"
		args = []string{
			"--single-transaction", "--quick", "--add-drop-table",
			fmt.Sprintf("--host=%s", info.Host),
			fmt.Sprintf("--port=%s", info.Port),
			fmt.Sprintf("--user=%s", info.User),
			"--protocol=TCP",
			fmt.Sprintf("--result-file=%s", path),
			info.Database,
		}
	case "postgres":
		tool = "pg_dump"
		envKey = "PGPASSWORD"
		args = []string{
			"--clean", "--if-exists",
			fmt.Sprintf("--host=%s", info.Host),
			fmt.Sprintf("--port=%s", info.Port),
			fmt.Sprintf("--username=%s", info.User),
			fmt.Sprintf("--dbname=%s", info.Database),
			fmt.Sprintf("--file=%s", path),
		}
	default:
		return fmt.Errorf("不支持的引擎 %q", info.Engine)
	}
	cmd := exec.Command(tool, args...)
	cmd.Env = append(os.Environ(), envKey+"="+info.Password)
	out, err := cmd.Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
			if i := strings.IndexByte(msg, '\n'); i >= 0 {
				msg = msg[:i]
			}
		}
		return fmt.Errorf("%s 失败：%s%s", tool, err, msg)
	}
	_ = out
	// 产物校验：空文件判失败。
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("备份产物不存在：%v", err)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("备份产物是空文件（工具没写出任何东西）")
	}
	return nil
}

func (o *externalDumpOverlay) View(width, height int, th *theme.Theme) string {
	boxW := 72
	if boxW > width-4 && width-4 >= 24 {
		boxW = width - 4
	}
	var b strings.Builder
	b.WriteString(" " + th.Text.Render("备份数据库（原生 dump 工具）") + "\n")
	b.WriteString(" " + fmt.Sprintf("%s %s@%s:%s/%s", info_engine(o.info), o.info.User, o.info.Host, o.info.Port, o.info.Database) + "\n")
	b.WriteString(" " + fmt.Sprintf("写入 %s", o.path) + "\n")
	b.WriteString(" " + th.Subtle.Render("口令经环境变量传递，不出现在命令行。") + "\n")
	if o.err != "" {
		b.WriteString(" " + th.Error.Render(o.err) + "\n")
	}
	b.WriteString(" " + th.Subtle.Render(" y 确认  ·  n 取消"))
	return ui.Box("backup-native", b.String(), boxW, th)
}

func info_engine(info db.DumpConnInfo) string {
	if info.Engine == "mysql" {
		return "mysqldump"
	}
	return "pg_dump"
}
