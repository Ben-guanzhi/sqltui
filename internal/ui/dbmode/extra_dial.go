package dbmode

// 波 100 续（sqltui 进程插件化）：非内置引擎改为「exe 旁的进程插件」——
// 每个连接起一个 sqltui-plugin-<kind> 进程，spec 走 argv[1]，Close 时进程
// 回收。默认构建不再需要 -tags extra_engines 就能扩展引擎；插件不存在或
// 握手失败时给出可操作提示（怎么放、放哪里）。
//
// 这是 §49 的 build-tag 版之上的运行时化：dialHook 先查插件目录，命中就
// 用 pluginbe.Dial 起进程；未命中再回落 extra_engines 的内置 dial（若该
// tag 编入），两者都没有才报错——给迁移留了兼容窗口。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/pluginbe"
)

// pluginPrefix / pluginExt name a sqltui driver plugin.
const pluginPrefix = "sqltui-plugin-"

func pluginExe(exeDir, kind string) string {
	name := pluginPrefix + kind
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(exeDir, name)
}

// extraDialer dials one non-builtin engine: first try a process plugin beside
// the executable; fall back to an in-process dial registered by the
// extra_engines build tag; otherwise report how to obtain the engine.
var extraDialers = map[string]func(ctx context.Context, v map[string]string) (db.Backend, db.KVBackend, error){}

// extraLabels holds the note shown when a kind has no provider at all.
var extraLabels = map[string]string{}

// dialHook routes a non-builtin engine's dial.
func dialHook(kind string, ctx context.Context, v map[string]string) (db.Backend, db.KVBackend, error) {
	// 1) Process plugin beside the executable — the runtime path.
	if exe, err := os.Executable(); err == nil {
		exePath := pluginExe(filepath.Dir(exe), kind)
		if _, err := os.Stat(exePath); err == nil {
			spec := pluginbe.ConnSpec{
				Kind:     kind,
				Host:     v[fkHost],
				Port:     v[fkPort],
				User:     v[fkUser],
				Password: v[fkPassword],
				Database: v[fkDatabase],
				SSLMode:  v[fkSslMode],
				Org:      v[fkOrg],
			}
			be, err := pluginbe.Dial(ctx, exePath, spec)
			if err != nil {
				return nil, nil, fmt.Errorf("plugin %s: %w", filepath.Base(exePath), err)
			}
			return be, nil, nil
		}
	}
	// 2) In-process dial (the -tags extra_engines build, kept for compatibility).
	if d, ok := extraDialers[kind]; ok {
		return d(ctx, v)
	}
	// 3) Neither: tell the user exactly what to put where.
	note := extraMissing(kind)
	if note == "" {
		note = fmt.Sprintf(
			"put %s beside the executable to add %s (or rebuild with -tags extra_engines)",
			pluginPrefix+kind+pluginSuffix(), kind)
	}
	return nil, nil, fmt.Errorf("%s is not available — %s", kind, note)
}

func pluginSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// extraMissing reports the note for kind, for the form to show before dialing.
func extraMissing(kind string) string { return extraLabels[kind] }

// PluginKinds lists the plugin engine kinds found beside the executable, so
// the form can offer them without hard-coding a list (波 100: discovery).
func PluginKinds() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Dir(exe))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if !strings.HasPrefix(base, pluginPrefix) {
			continue
		}
		kind := strings.ToLower(strings.TrimPrefix(base, pluginPrefix))
		if kind != "" {
			out = append(out, kind)
		}
	}
	return out
}
