// Package sqlserverbe connects to a SQL Server instance with a split
// integration: dialing, statement routing, and execution go through the
// dbcore kernel like every other engine, while the cross-database catalog
// (sys.databases, INFORMATION_SCHEMA filtered by TABLE_CATALOG, three-part
// table names) stays local — the kernel's catalog is scoped to the connected
// database and has no cross-database browsing yet. The local queries run
// through the kernel driver with bound parameters, so no values are ever
// concatenated.
package sqlserverbe

import (
	"context"
	"fmt"
	"strings"

	"github.com/dbcore/dbcore"
	_ "github.com/dbcore/dbcore/mssql" // registers the mssql factory

	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/core"
)

// Config holds the connection parameters for a SQL Server database.
type Config struct {
	UserName string
	Password string
	Host     string
	Port     string
	DbName   string
}

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

// ConnSpec renders the config as a kernel connection spec. TLS stays off
// (SSLMode "disable"), preserving the pre-kernel DSN's encrypt=disable
// default for servers with older TLS versions.
func (c Config) ConnSpec() dbcore.ConnSpec {
	return dbcore.ConnSpec{
		Kind:           dbcore.KindMSSQL,
		Host:           strings.TrimSpace(c.Host),
		Port:           core.ParsePort(c.Port),
		User:           c.UserName,
		Password:       c.Password,
		Database:       strings.TrimSpace(c.DbName),
		SSLMode:        "disable",
		ConnectTimeout: core.QueryTimeout,
	}
}

// Backend adapts the kernel mssql driver to the db.Backend contract.
type Backend struct {
	*core.Backend
	drv dbcore.SQLDriver
}

var (
	_ db.Backend       = (*Backend)(nil)
	_ db.TableTargeter = (*Backend)(nil)
)

// Connect opens a SQL Server connection through the kernel, validates the
// config, pings the server, and returns a ready-to-use Backend.
func Connect(cfg Config) (*Backend, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("sqlserver connect: invalid config: %w", err)
	}
	drv, err := dbcore.Open(context.Background(), cfg.ConnSpec(), dbcore.PoolConfig{})
	if err != nil {
		return nil, fmt.Errorf("sqlserver connect: %w", err)
	}
	sqlDrv, ok := drv.AsSQL()
	if !ok {
		drv.Close()
		return nil, fmt.Errorf("sqlserver: kernel driver is not an SQL driver")
	}
	return &Backend{
		Backend: core.New(sqlDrv, core.Options{
			Kind:    "sqlserver",
			Title:   fmt.Sprintf("sqlserver://%s/%s", strings.TrimSpace(cfg.Host), strings.TrimSpace(cfg.DbName)),
			Target:  Target,
			CurrentNS: func(context.Context) string { return strings.TrimSpace(cfg.DbName) },
		}),
		drv: sqlDrv,
	}, nil
}

// Target spells namespace.table as [ns].dbo.[tbl] (SQL Server wants
// three-part naming, so a non-empty namespace defaults to the dbo schema);
// an empty namespace yields just [tbl].
func Target(namespace, table string) string {
	ident := core.QuoteIdent("mssql", table)
	if namespace != "" {
		return core.QuoteIdent("mssql", namespace) + ".dbo." + ident
	}
	return ident
}

// Namespaces lists the databases on the server (cross-database browsing; the
// kernel only lists schemas of the connected database).
func (b *Backend) Namespaces() ([]string, error) {
	res, err := b.query("SELECT name FROM sys.databases ORDER BY name")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(res.Rows))
	for _, r := range res.Rows {
		if len(r) > 0 {
			out = append(out, r[0].S)
		}
	}
	return out, nil
}

// Tables lists the base tables of one database via the TABLE_CATALOG-filtered
// catalog view; an empty namespace falls back to the connected database (the
// pre-kernel code built an invalid `[].INFORMATION_SCHEMA…` reference then).
func (b *Backend) Tables(namespace string) ([]string, error) {
	query := `SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'`
	args := []dbcore.Value{}
	if namespace != "" {
		query += ` AND TABLE_CATALOG = @p1`
		args = append(args, textArg(namespace))
	}
	res, err := b.query(query+" ORDER BY TABLE_NAME", args...)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(res.Rows))
	for _, r := range res.Rows {
		if len(r) > 0 {
			out = append(out, r[0].S)
		}
	}
	return out, nil
}

// PrimaryKeys lists the primary-key column names of database.table in
// key-ordinal order, across the named database (empty = current database).
// The join through TABLE_CONSTRAINTS is the portable spelling: the old
// OBJECTPROPERTY(OBJECT_ID(<table>), 'IsPrimaryKey') trick tested that
// property against the table object, where it is always 0 — it is a
// constraint-object property — so PrimaryKeys returned empty for every
// table (caught by the live smoke).
func (b *Backend) PrimaryKeys(namespace, table string) ([]string, error) {
	query := `SELECT ku.COLUMN_NAME
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
		JOIN INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
			AND tc.TABLE_CATALOG = ku.TABLE_CATALOG
			AND tc.TABLE_SCHEMA = ku.TABLE_SCHEMA
			AND tc.TABLE_NAME = ku.TABLE_NAME
		WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
		AND ku.TABLE_NAME = @p1`
	args := []dbcore.Value{textArg(table)}
	if namespace != "" {
		query += ` AND ku.TABLE_CATALOG = @p2`
		args = append(args, textArg(namespace))
	}
	res, err := b.query(query+" ORDER BY ku.ORDINAL_POSITION", args...)
	if err != nil {
		return nil, err
	}
	pks := make([]string, 0, len(res.Rows))
	for _, r := range res.Rows {
		if len(r) > 0 {
			pks = append(pks, r[0].S)
		}
	}
	return pks, nil
}

// ColumnsMeta returns column metadata for database.table in ordinal order,
// across the named database (empty = current database).
func (b *Backend) ColumnsMeta(namespace, table string) ([]db.ColumnMeta, error) {
	query := `SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_NAME = @p1`
	args := []dbcore.Value{textArg(table)}
	if namespace != "" {
		query += ` AND TABLE_CATALOG = @p2`
		args = append(args, textArg(namespace))
	}
	res, err := b.query(query+" ORDER BY ORDINAL_POSITION", args...)
	if err != nil {
		return nil, err
	}
	cols := make([]db.ColumnMeta, 0, len(res.Rows))
	for _, r := range res.Rows {
		if len(r) < 4 {
			continue
		}
		c := db.ColumnMeta{Name: r[0].S, DataType: r[1].S, IsNullable: r[2].S}
		if s, ok := dbcore.TryGetString(r[3]); ok {
			c.Default = s
		}
		cols = append(cols, c)
	}
	return cols, nil
}

// ColumnIndexTypes returns a column→index-type map for database.table.
// Priority: PK > UNIQUE > INDEX. (The pre-kernel query ordered by
// is_primary_key but never labeled PK, so primary-key columns showed up as
// UNIQUE; this one reads the flag.)
func (b *Backend) ColumnIndexTypes(namespace, table string) (map[string]string, error) {
	res, err := b.query(`SELECT
		i.name AS index_name,
		COL_NAME(ic.object_id, ic.column_id) AS column_name,
		CAST(i.is_unique AS int) AS is_unique,
		CAST(i.is_primary_key AS int) AS is_pk
		FROM sys.indexes i
		INNER JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		WHERE i.object_id = OBJECT_ID(@p1 + '.dbo.' + @p2)
		AND i.type > 0
		ORDER BY CASE WHEN i.is_primary_key = 1 THEN 0 WHEN i.is_unique = 1 THEN 1 ELSE 2 END`,
		textArg(namespace), textArg(table))
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, r := range res.Rows {
		if len(r) < 4 {
			continue
		}
		colName := r[1].S
		label := "INDEX"
		if r[3].I == 1 {
			label = "PK"
		} else if r[2].I == 1 {
			label = "UNIQUE"
		}
		if existing, ok := result[colName]; !ok || db.IndexPriority(existing) < db.IndexPriority(label) {
			result[colName] = label
		}
	}
	return result, nil
}

// FetchTable, Run, TableTarget, quoting, and Close are inherited from the
// core adapter: TOP limiting and statement routing come from the kernel;
// only the [db].dbo.[table] target spelling is engine-local.

func (b *Backend) query(sql string, args ...dbcore.Value) (*dbcore.QueryResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), core.QueryTimeout)
	defer cancel()
	return b.drv.Execute(ctx, sql, args...)
}

// textArg wraps s as a bound kernel parameter.
func textArg(s string) dbcore.Value {
	return dbcore.Value{Kind: dbcore.ValText, S: s}
}
