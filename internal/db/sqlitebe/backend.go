// Package sqlitebe opens SQLite database files through the dbcore kernel:
// the kernel owns the connection, statement routing, and every catalog query;
// this wrapper only carries the UI presentation semantics. (The file loader
// in internal/reader stays kernel-free by design: it is a reader, not a
// client.)
package sqlitebe

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/dbcore/dbcore"
	_ "github.com/dbcore/dbcore/sqlite" // registers the sqlite factory

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/core"
)

// Config holds the connection parameters for a sqlite database file.
type Config struct {
	FilePath string
}

// Backend adapts the kernel sqlite driver to the db.Backend contract.
type Backend struct {
	*core.Backend
}

var (
	_ db.Backend       = (*Backend)(nil)
	_ db.TableTargeter = (*Backend)(nil)
)

// Connect opens the sqlite file through the kernel and pings it before
// returning. The file must already exist: silently creating one from a typo
// in the path used to strand users in an empty database.
func Connect(cfg Config) (*Backend, error) {
	file := strings.TrimSpace(cfg.FilePath)
	if file == "" {
		return nil, fmt.Errorf("sqlite file path is empty")
	}
	if _, err := os.Stat(file); err != nil {
		return nil, err
	}
	drv, err := dbcore.Open(context.Background(), dbcore.ConnSpec{
		Kind: dbcore.KindSQLite,
		File: file,
	}, dbcore.PoolConfig{})
	if err != nil {
		return nil, err
	}
	sql, ok := drv.AsSQL()
	if !ok {
		drv.Close()
		return nil, fmt.Errorf("sqlite: kernel driver is not an SQL driver")
	}
	return &Backend{Backend: core.New(sql, core.Options{
		Kind:            "sqlite",
		Title:           "sqlite://" + file,
		Target:          Target,
		HasLastInsertID: true,
	})}, nil
}

// Target spells the table as "tbl" (kernel-quoted); SQLite files have no
// schema level, so the namespace argument is ignored.
func Target(_, table string) string {
	return core.QuoteIdent("sqlite", table)
}
