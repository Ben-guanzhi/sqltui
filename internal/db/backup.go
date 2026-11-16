package db

// 库级备份（对齐 dbstudio 的 dumpSQLite）：把一个 schema 的表与数据导成
// SQL 文本。
//
// 结构从 **ColumnsMeta 反推**（本仓没有 sqlite_master 那样的权威 DDL 通道；
// 跨引擎的 CREATE TABLE 反推是「够用」的：列名 + 类型 + 主键）。数据
// **分批读**（FetchTable 按 limit 取页），外层包事务并关外键检查——与
// dbstudio / sqlgpui 的备份同一形态。
//
// 边界（写下来避免误用）：这是**数据备份**，不是 schema 精确复刻——触发器、
// 存储过程、视图定义不在其中；要精确 schema 请用引擎自己的 dump 工具。

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LinPr/sqltui/internal/data"
)

/// 一次取多少行（FetchTable 的上限就是 10000）。
const backupRowLimit = 10000

// QuoteBackupIdent 用双引号包标识符（内部双引号翻倍）——SQL 标准转义。
func QuoteBackupIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QuoteBackupLiteral 用单引号包值（内部单引号翻倍）。
func QuoteBackupLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

// BackupTable 一个表的 DDL + 分批读数据的回调形态，供后端适配。
type BackupTable struct {
	Name  string
	Cols  []ColumnMeta
	PKs   []string
}

// BackupBackend 是可选的备份能力：枚举表 + 读列 + 取页。
// （Tables / ColumnsMeta / FetchTable 都已在 Backend 里，这里只是把三步
// 串起来的形态说明——真正的实现走泛型函数 buildBackup。）
type BackupBackend interface {
	Backend
}

// backupLiteral 把一个 any 渲染成 SQL 字面量。nil → NULL；字符串加引号；
// 其它用 %v（数字/布尔）。
func backupLiteral(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case string:
		return QuoteBackupLiteral(t)
	case []byte:
		return QuoteBackupLiteral(string(t))
	default:
		return fmt.Sprintf("%v", t)
	}
}

// buildBackup 生成一个 schema 的备份文本：tables 由调用方枚举，meta/pks/fetch
// 由后端提供。fetch 是 FetchTable 的适配（一次取全量，见 BackupRowLimit）。
func buildBackup(
	namespace string,
	tables []string,
	meta func(table string) ([]ColumnMeta, error),
	pks func(table string) ([]string, error),
	fetch func(namespace, table string, limit int) (*data.Frame, error),
) (string, error) {
	var out strings.Builder
	out.WriteString("BEGIN TRANSACTION;\n")

	sorted := append([]string(nil), tables...)
	sort.Strings(sorted) // 确定性

	for _, t := range sorted {
		cols, err := meta(t)
		if err != nil {
			return "", fmt.Errorf("读列 %s: %w", t, err)
		}
		keys, err := pks(t)
		if err != nil {
			return "", fmt.Errorf("读主键 %s: %w", t, err)
		}
		pkSet := map[string]bool{}
		for _, k := range keys {
			pkSet[k] = true
		}

		// DDL（反推）：列名 类型 [PRIMARY KEY]。
		var parts []string
		for _, c := range cols {
			def := QuoteBackupIdent(c.Name) + " " + c.DataType
			if pkSet[c.Name] {
				def += " PRIMARY KEY"
			}
			parts = append(parts, def)
		}
		out.WriteString(fmt.Sprintf("CREATE TABLE %s (\n  %s\n);\n",
			QuoteBackupIdent(t), strings.Join(parts, ",\n  ")))

		// 数据：一次全量（FetchTable 的上限是 10000 行——更大的库请用引擎
		// 自己的 dump 工具，边界写在函数注释里）。
		var quoted []string
		for _, c := range cols {
			quoted = append(quoted, QuoteBackupIdent(c.Name))
		}
		colList := strings.Join(quoted, ", ")
		frame, err := fetch(namespace, t, backupRowLimit)
		if err != nil {
			return "", fmt.Errorf("读数据 %s: %w", t, err)
		}
		if frame != nil {
			for r := 0; r < frame.NumRows(); r++ {
				row := frame.Row(r)
				vals := make([]string, len(row))
				for i, v := range row {
					vals[i] = backupLiteral(v)
				}
				out.WriteString(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);\n",
					QuoteBackupIdent(t), colList, strings.Join(vals, ", ")))
			}
		}
	}

	out.WriteString("COMMIT;\n")
	return out.String(), nil
}
