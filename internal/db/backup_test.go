package db

// 备份的排版：DDL 反推、字面量转义、确定性。

import (
	"strings"
	"testing"

	"github.com/LinPr/sqltui/internal/data"
)

func TestBackupLiteralEscapes(t *testing.T) {
	if got := backupLiteral(nil); got != "NULL" {
		t.Errorf("nil = %q, want NULL", got)
	}
	if got := backupLiteral("a'b"); got != "'a''b'" {
		t.Errorf("string = %q", got)
	}
	if got := backupLiteral([]byte("x")); got != "'x'" {
		t.Errorf("bytes = %q", got)
	}
	if got := backupLiteral(42); got != "42" {
		t.Errorf("int = %q", got)
	}
}

func TestBackupBuildsDDLAndInserts(t *testing.T) {
	meta := func(string) ([]ColumnMeta, error) {
		return []ColumnMeta{{Name: "id", DataType: "INTEGER"}, {Name: "name", DataType: "TEXT"}}, nil
	}
	pks := func(string) ([]string, error) { return []string{"id"}, nil }
	frame := data.New("id", "name")
	frame.AppendRow([]any{int64(1), "Ada"})
	frame.AppendRow([]any{int64(2), "a'b"})
	fetch := func(ns, table string, limit int) (*data.Frame, error) { return frame, nil }

	out, err := buildBackup("main", []string{"t"}, meta, pks, fetch)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"BEGIN TRANSACTION;",
		`CREATE TABLE "t" (` + "\n" + `  "id" INTEGER PRIMARY KEY`,
		`INSERT INTO "t" ("id", "name") VALUES (1, 'Ada');`,
		// 字面量转义：值里的单引号翻倍。
		`VALUES (2, 'a''b');`,
		"COMMIT;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("备份里没有 %q：\n%s", want, out)
		}
	}
	// 确定性：两次相同。
	out2, _ := buildBackup("main", []string{"t"}, meta, pks, fetch)
	if out != out2 {
		t.Error("两次备份不同（应为确定性）")
	}
}
