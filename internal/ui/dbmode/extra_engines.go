//go:build extra_engines

package dbmode

// 波 100 插件化的 extra_engines 构建：postgres/redis/sqlserver/oracle 四个
// 引擎随二进制编译（它们的 be 包各自引 dbcore 的驱动并注册 factory）。默认
// 构建没有这个 tag——二进制里只有 mysql/sqlite，体积小一半以上。

import (
	"context"

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/oraclebe"
	"github.com/LinPr/sqltui/internal/db/postgresbe"
	"github.com/LinPr/sqltui/internal/db/redisbe"
	"github.com/LinPr/sqltui/internal/db/sqlserverbe"
)

func init() {
	extraDialers["postgres"] = func(ctx context.Context, v map[string]string) (db.Backend, db.KVBackend, error) {
		be, err := postgresbe.Connect(ctx, postgresbe.Config{
			UserName: v[fkUser], Password: v[fkPassword],
			Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
			SslMode: v[fkSslMode],
		})
		if err != nil {
			return nil, nil, err
		}
		return be, nil, nil
	}
	extraDialers["redis"] = func(ctx context.Context, v map[string]string) (db.Backend, db.KVBackend, error) {
		kv, err := redisbe.Connect(ctx, redisbe.Config{
			UserName: v[fkUser], Password: v[fkPassword],
			Host: v[fkHost], Port: v[fkPort], RdbNum: v[fkDbNum],
		})
		if err != nil {
			return nil, nil, err
		}
		return nil, kv, nil
	}
	extraDialers["sqlserver"] = func(ctx context.Context, v map[string]string) (db.Backend, db.KVBackend, error) {
		be, err := sqlserverbe.Connect(ctx, sqlserverbe.Config{
			UserName: v[fkUser], Password: v[fkPassword],
			Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
		})
		if err != nil {
			return nil, nil, err
		}
		return be, nil, nil
	}
	extraDialers["oracle"] = func(ctx context.Context, v map[string]string) (db.Backend, db.KVBackend, error) {
		be, err := oraclebe.Connect(ctx, oraclebe.Config{
			UserName: v[fkUser], Password: v[fkPassword],
			Host: v[fkHost], Port: v[fkPort], DbName: v[fkDatabase],
		})
		if err != nil {
			return nil, nil, err
		}
		return be, nil, nil
	}
}
