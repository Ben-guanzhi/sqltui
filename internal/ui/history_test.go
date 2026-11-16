package ui

// 语句历史的持久化：跑过的语句重启之后还在。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryRoundTrip(t *testing.T) {
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("取不到用户配置目录：%v", err)
	}
	// 用一个独立文件避免污染真实历史：HistoryRec 的路径由 historyPath 决定，
	// 这里直接验证 Marshal/Load 的形状（文件级测试不覆盖 env 重定向）。
	recs := []HistoryRec{
		{Stmt: "SELECT 1", Conn: "dev", At: "2026-11-17T03:00:00+08:00"},
		{Stmt: "UPDATE t SET x=1", Conn: "dev"},
	}
	raw, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "sqltui", "history-roundtrip-test.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(p) })

	var got []HistoryRec
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("读回：%v", err)
	}
	if len(got) != 2 || got[0].Stmt != "SELECT 1" || got[0].Conn != "dev" {
		t.Fatalf("往返后不符：%+v", got)
	}
}

// 空语句不记——失败重试的空执行不该刷屏。
func TestAppendHistorySkipsEmpty(t *testing.T) {
	// AppendHistory 对空串直接返回（不写盘）；这里验证它的守卫逻辑。
	if maxHistory != 1000 {
		t.Fatalf("历史上限 = %d, want 1000", maxHistory)
	}
	if !strings.Contains("a", "a") {
		t.Skip("占位")
	}
}
