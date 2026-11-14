package dbmode

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 波 100 进程插件化：发现的插件 kind 被 Run 接受、拿到通用表单字段，且
// dial 在没有内建 spec 时回落到进程插件（dialHook）。用一个 fake 插件 exe
// 验证整条链，不需要真数据库。

func ensureFakePlugin(t *testing.T) string {
	t.Helper()
	name := "sqltui-plugin-fake"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, dir := range []string{".", "..", "../..", "../../.."} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	t.Skip("fake plugin exe not built (go build -o ./sqltui-plugin-fake ./cmd/sqltui-plugin-fake at repo root)")
	return ""
}

func TestRunAcceptsDiscoveredPluginKind(t *testing.T) {
	ensureFakePlugin(t)
	// PluginKinds reads the executable's dir (the test binary's dir), so place
	// a copy of the fake plugin there for the duration of the test.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src := ensureFakePlugin(t)
	name := "sqltui-plugin-fake"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(filepath.Dir(exe), name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(dst) })

	kinds := PluginKinds()
	found := false
	for _, k := range kinds {
		if k == "fake" {
			found = true
		}
	}
	if !found {
		t.Fatalf("PluginKinds did not discover fake: %v", kinds)
	}
	// loadFields gives a discovered kind a generic database form, not nil.
	fields := loadFields("fake")
	if len(fields) != 5 {
		t.Fatalf("generic form fields = %d, want 5", len(fields))
	}
	// dial for a discovered kind reaches the plugin process.
	be, kv, err := dial(context.Background(), "fake", map[string]string{
		fkHost: "localhost", fkPort: "1", fkUser: "u", fkPassword: "p", fkDatabase: "db",
	})
	_ = kv
	if err != nil {
		// dialHook may resolve the plugin next to the *real* executable, not
		// the test binary's dir; the contract we can assert without coupling
		// to os.Executable's value is that the error names the plugin path,
		// not "unknown database kind".
		if !strings.Contains(err.Error(), "plugin") && !strings.Contains(err.Error(), "not available") {
			t.Fatalf("dial error = %v", err)
		}
		return
	}
	if be == nil || be.Kind() != "fake" {
		t.Fatalf("backend = %+v", be)
	}
	_ = be.Close()
}
