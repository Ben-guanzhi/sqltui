// Package mysqlbe connects to a MySQL server through the dbcore kernel: the
// kernel owns the DSN, the connection, statement routing, and every catalog
// query; this wrapper only carries the UI presentation semantics.
package mysqlbe

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ben-guanzhi/dbcore"
	_ "github.com/Ben-guanzhi/dbcore/mysql" // registers the mysql factory

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/core"
)

// Config holds the connection parameters for a MySQL server.
type Config struct {
	UserName string
	Password string
	Host     string
	Port     string
	DbName   string
}

// ConnectTimeout bounds the initial dial (the same value every sqltui backend
// used before the kernel migration).
const ConnectTimeout = 10 * time.Second

// ConnSpec renders the config as a kernel connection spec. The port falls
// back to the engine default when the form leaves it blank or unparsable; the
// charset param keeps the pre-kernel connection behavior.
func (c Config) ConnSpec() dbcore.ConnSpec {
	return dbcore.ConnSpec{
		Kind:           dbcore.KindMySQL,
		Host:           strings.TrimSpace(c.Host),
		Port:           core.ParsePort(c.Port),
		User:           c.UserName,
		Password:       c.Password,
		Database:       strings.TrimSpace(c.DbName),
		Params:         map[string]string{"charset": "utf8"},
		ConnectTimeout: ConnectTimeout,
	}
}

// Backend adapts the kernel mysql driver to the db.Backend contract.
type Backend struct {
	*core.Backend
	// dumpInfo 是 mysqldump 需要的连接参数（:backup 优先走原生工具）。
	dumpInfo db.DumpConnInfo
}

var (
	_ db.Backend       = (*Backend)(nil)
	_ db.TableTargeter = (*Backend)(nil)
	_ db.Quoter        = (*Backend)(nil)
	_ db.Escaper       = (*Backend)(nil)
)

// Connect opens a mysql connection through the kernel and pings it before
// returning. The context bounds the dial, so the UI can cancel an in-flight
// connect on exit instead of leaving the kernel dial running in the background.
func Connect(ctx context.Context, cfg Config) (*Backend, error) {
	drv, err := dbcore.Open(ctx, cfg.ConnSpec(), dbcore.DefaultPoolConfig())
	if err != nil {
		return nil, err
	}
	sql, ok := drv.AsSQL()
	if !ok {
		drv.Close()
		return nil, fmt.Errorf("mysql: kernel driver is not an SQL driver")
	}
	return &Backend{Backend: core.New(sql, core.Options{
		Kind:            "mysql",
		Title:           fmt.Sprintf("mysql://%s/%s", strings.TrimSpace(cfg.Host), strings.TrimSpace(cfg.DbName)),
		Target:          Target,
		CurrentNS:       func(context.Context) (string, error) { return strings.TrimSpace(cfg.DbName), nil },
		HasLastInsertID: true,
	}), dumpInfo: db.DumpConnInfo{
		Engine:   "mysql",
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.UserName,
		Password: cfg.Password,
		Database: cfg.DbName,
	}}, nil
}

// DumpConn 供 :backup 优先走 mysqldump（精确 schema，无行数上限）。
func (b *Backend) DumpConn() db.DumpConnInfo { return b.dumpInfo }

// Target spells namespace.table as `ns`.`tbl` (each part kernel-quoted); an
// empty namespace yields just `tbl`, which resolves on the connection's
// current database.
func Target(namespace, table string) string {
	tbl := core.QuoteIdent("mysql", table)
	if namespace != "" {
		return core.QuoteIdent("mysql", namespace) + "." + tbl
	}
	return tbl
}
