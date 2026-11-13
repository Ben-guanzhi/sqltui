// Package postgresbe connects to a PostgreSQL server through the dbcore
// kernel: the kernel owns the DSN, the connection (pooled *sql.DB rather than
// the old single pgx.Conn), statement routing, and every catalog query; this
// wrapper only carries the UI presentation semantics.
package postgresbe

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dbcore/dbcore"
	_ "github.com/dbcore/dbcore/postgres" // registers the postgres factory

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/core"
)

// Config holds the connection parameters for a PostgreSQL server.
type Config struct {
	UserName string
	Password string
	Host     string
	Port     string
	DbName   string
	SslMode  string
}

// ConnectTimeout bounds the initial dial (the same value every sqltui backend
// used before the kernel migration).
const ConnectTimeout = 10 * time.Second

// ConnSpec renders the config as a kernel connection spec. An empty SslMode
// becomes "disable", matching the pre-kernel DSN builder; the kernel maps the
// portability spellings onto libpq's own.
func (c Config) ConnSpec() dbcore.ConnSpec {
	ssl := strings.TrimSpace(c.SslMode)
	if ssl == "" {
		ssl = "disable"
	}
	return dbcore.ConnSpec{
		Kind:           dbcore.KindPostgres,
		Host:           strings.TrimSpace(c.Host),
		Port:           core.ParsePort(c.Port),
		User:           c.UserName,
		Password:       c.Password,
		Database:       strings.TrimSpace(c.DbName),
		SSLMode:        ssl,
		ConnectTimeout: ConnectTimeout,
	}
}

// Backend adapts the kernel postgres driver to the db.Backend contract.
type Backend struct {
	*core.Backend
}

var (
	_ db.Backend       = (*Backend)(nil)
	_ db.TableTargeter = (*Backend)(nil)
)

// Connect opens a postgres connection through the kernel and pings it before
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
		return nil, fmt.Errorf("postgres: kernel driver is not an SQL driver")
	}
	return &Backend{Backend: core.New(sql, core.Options{
		Kind:   "postgres",
		Title:  fmt.Sprintf("postgres://%s/%s", strings.TrimSpace(cfg.Host), strings.TrimSpace(cfg.DbName)),
		Target: Target,
		// The browser scopes its listing to the schema new objects resolve to
		// (usually "public"); the query keeps working after ALTER ROLE ... SET
		// search_path, unlike a hard-coded name.
		CurrentNS: core.CurrentNSQuery(sql, "SELECT current_schema()"),
	})}, nil
}

// Target spells namespace.table as "ns"."tbl" with kernel-quoted parts; an
// empty namespace yields just "tbl", which resolves on the search path.
func Target(namespace, table string) string {
	tbl := core.QuoteIdent("postgres", table)
	if namespace != "" {
		return core.QuoteIdent("postgres", namespace) + "." + tbl
	}
	return tbl
}
