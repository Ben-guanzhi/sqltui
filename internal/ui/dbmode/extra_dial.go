package dbmode

import (
	"context"
	"fmt"

	"github.com/LinPr/sqltui/internal/db"
)

// extraDialer dials one non-builtin engine. 波 100 插件化：默认构建只有
// MySQL 与 SQLite 内置；postgres/redis/sqlserver/oracle 的 dial 由
// `-tags extra_engines` 构建的 extra_engines.go 注册进来。没有 hook 时，
// 表单对这些 kind 的连接请求得到一句怎么获得引擎的说明——比内核的
// "unknown kind" 多说一句。
var extraDialers = map[string]func(ctx context.Context, v map[string]string) (db.Backend, db.KVBackend, error){}

// extraLabels 报告某个非内置 kind 在此构建下的获取方式；extra_engines 构建
// 覆盖它为空串（引擎可用，无需说明）。
var extraLabels = map[string]string{}

// dialHook routes a non-builtin engine's dial: registered extra dialer wins,
// otherwise the form tells the user how the engine is delivered.
func dialHook(kind string, ctx context.Context, v map[string]string) (db.Backend, db.KVBackend, error) {
	if d, ok := extraDialers[kind]; ok {
		return d(ctx, v)
	}
	return nil, nil, fmt.Errorf(
		"%s is not built into this binary — rebuild with -tags extra_engines, or run the full build from the releases page",
		kind)
}

// extraMissing reports the note for kind, for the form to show before dialing.
func extraMissing(kind string) string { return extraLabels[kind] }
