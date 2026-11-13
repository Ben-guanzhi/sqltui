// Package oraclebe connects to an Oracle database through the dbcore kernel:
// the kernel owns the DSN (URL-escaped credentials), the connection,
// statement routing, and every catalog query; this wrapper only carries the
// UI presentation semantics.
package oraclebe

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dbcore/dbcore"
	_ "github.com/dbcore/dbcore/oracle" // registers the oracle factory

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/core"
)

// Config holds the connection parameters for an Oracle database.
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

// Validate checks that all required fields are present and returns an error
// describing the first missing field, or nil if the config is valid.
func (c Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Port == "" {
		return fmt.Errorf("port is required")
	}
	if c.UserName == "" {
		return fmt.Errorf("username is required")
	}
	if c.DbName == "" {
		return fmt.Errorf("database name is required")
	}
	return nil
}

// ConnSpec renders the config as a kernel connection spec; the kernel's DSN
// builder URL-escapes credentials and the service name, so special characters
// in passwords survive.
func (c Config) ConnSpec() dbcore.ConnSpec {
	return dbcore.ConnSpec{
		Kind:           dbcore.KindOracle,
		Host:           strings.TrimSpace(c.Host),
		Port:           core.ParsePort(c.Port),
		User:           c.UserName,
		Password:       c.Password,
		Database:       strings.TrimSpace(c.DbName),
		ConnectTimeout: ConnectTimeout,
	}
}

// Backend adapts the kernel oracle driver to the db.Backend contract.
type Backend struct {
	*core.Backend
}

var (
	_ db.Backend       = (*Backend)(nil)
	_ db.TableTargeter = (*Backend)(nil)
)

// Connect opens an Oracle connection through the kernel, validates the
// config, pings the server, and returns a ready-to-use Backend. The context
// bounds the dial, so the UI can cancel an in-flight connect on exit.
func Connect(ctx context.Context, cfg Config) (*Backend, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("oracle connect: invalid config: %w", err)
	}
	drv, err := dbcore.Open(ctx, cfg.ConnSpec(), dbcore.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("oracle connect: %w", err)
	}
	sql, ok := drv.AsSQL()
	if !ok {
		drv.Close()
		return nil, fmt.Errorf("oracle: kernel driver is not an SQL driver")
	}
	return &Backend{Backend: core.New(sql, core.Options{
		Kind:      "oracle",
		Title:     fmt.Sprintf("oracle://%s/%s", strings.TrimSpace(cfg.Host), strings.TrimSpace(cfg.DbName)),
		Target:    Target,
		CurrentNS: func(context.Context) (string, error) { return strings.TrimSpace(cfg.UserName), nil },
	})}, nil
}

// Target spells namespace.table as "ns"."tbl" (kernel-quoted; empty namespace
// drops the owner qualifier, resolving on the login user's schema).
func Target(namespace, table string) string {
	tbl := core.QuoteIdent("oracle", table)
	if namespace != "" {
		return core.QuoteIdent("oracle", namespace) + "." + tbl
	}
	return tbl
}
