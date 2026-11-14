package pluginbe

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 波 100 进程插件化的端到端契约：一个真正的子进程走完 banner 握手 →
// 每个 Backend 方法一次 RPC → Close 回收进程。fake 插件不带数据库，验证的
// 是协议与生命周期，不是 SQL。

func fakePluginExe(t *testing.T) string {
	t.Helper()
	name := "sqltui-plugin-fake"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// The test binary lives in a temp dir; the fake plugin was built into the
	// module root by the test driver (see the build step in the wave notes).
	for _, dir := range []string{".", "..", "../..", "../../.."} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	t.Skipf("build cmd/sqltui-plugin-fake first (go build -o ./sqltui-plugin-fake ./cmd/sqltui-plugin-fake); not found")
	return ""
}

func TestPluginBackendEndToEnd(t *testing.T) {
	exe := fakePluginExe(t)
	spec := ConnSpec{Kind: "fake", Host: "localhost", Port: "1", User: "u", Password: "p", Database: "db"}
	be, err := Dial(context.Background(), exe, spec)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if be.Kind() != "fake" {
		t.Errorf("Kind = %q", be.Kind())
	}
	if be.Title() != "fake://localhost" {
		t.Errorf("Title = %q", be.Title())
	}
	if ns, err := be.Namespaces(); err != nil || len(ns) != 1 || ns[0] != "main" {
		t.Errorf("Namespaces = %v, %v", ns, err)
	}
	if tabs, err := be.Tables("main"); err != nil || len(tabs) != 2 {
		t.Errorf("Tables = %v, %v", tabs, err)
	}
	r, err := be.Run("SELECT 1")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.Frame == nil || r.Frame.NumCols() != 2 {
		t.Fatalf("Run frame = %+v", r.Frame)
	}
	if got := r.Frame.Columns[1].Cell(0); got != int64(42) {
		t.Errorf("int cell over the wire = %#v (%T)", got, got)
	}
	if pk, err := be.PrimaryKeys("main", "t1"); err != nil || len(pk) != 1 || pk[0] != "id" {
		t.Errorf("PrimaryKeys = %v, %v", pk, err)
	}
	if cm, err := be.ColumnsMeta("main", "t1"); err != nil || len(cm) != 1 || cm[0].DataType != "int" {
		t.Errorf("ColumnsMeta = %v, %v", cm, err)
	}
	if idx, err := be.ColumnIndexTypes("main", "t1"); err != nil || idx["id"] != "PK" {
		t.Errorf("ColumnIndexTypes = %v, %v", idx, err)
	}
	f, err := be.FetchTable("main", "t1", 10)
	if err != nil || f == nil || f.NumCols() != 1 {
		t.Errorf("FetchTable = %v, %v", f, err)
	}
	if err := be.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// A second Close is a no-op (no double Wait on the process).
	if err := be.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestPluginDialRejectsWrongKind(t *testing.T) {
	exe := fakePluginExe(t)
	_, err := Dial(context.Background(), exe, ConnSpec{Kind: "postgres"})
	if err == nil {
		t.Fatal("a fake plugin must be refused for a postgres connection")
	}
}
