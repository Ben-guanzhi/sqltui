# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Bubbletea-based TUI database client
- Multi-engine support: MySQL, PostgreSQL, SQLite, SQL Server, Oracle, Redis
- OpenObserve (OOBE) log backend integration
- Plot/visualization for query results
- Cell editing with dialect-aware quoting/escaping
- Query history and navigation

### Changed
- Cell edits and row deletes execute as parameter-bound statements (`db.BoundRunner` capability, kernel `Execute` with bind arguments): user text never reaches the statement text, PK values bind in their native types, and new values are parsed by the column type before binding. Backends without binding (OpenObserve) keep the inline-literal path; the placeholder spelling (`?`/`$n`/`@pn`/`:n`) comes from the kernel dialect
- SQL engines and Redis dial through the shared dbcore kernel (`dbcore.Open` + per-engine `ConnSpec`): statement routing, catalog metadata, identifier quoting, literals, and row limits come from the kernel; the per-engine DAOs (~1,700 lines of catalog SQL) are gone
- Fixed by live smoke against real servers (gate: `SQLTUI_SMOKE_*` env vars): SQL Server's primary-key listing was always empty — the old `OBJECTPROPERTY(OBJECT_ID(<table>), 'IsPrimaryKey')` filter tested a constraint-object property against the table; it now joins `TABLE_CONSTRAINTS`. SQL Server's index map lost PK/UNIQUE labels because `bit` columns scan as bool; the flags are `CAST` to int. The SQL Server wrapper also registers the kernel mssql factory it depends on (missing blank import made every SQL Server connection fail with `ERR_NOT_SUPPORTED`)
- Read-statement routing gained `DESC`/`DESCRIBE` and `INSERT`/`UPDATE`/`DELETE ... RETURNING` (kernel `IsReadQuery`, contributed upstream)
- Query results keep native types: NULL is no longer collapsed to the empty string on MySQL/PostgreSQL/SQLite
- PostgreSQL connections are pooled (`*sql.DB` via the kernel) instead of a single `pgx.Conn`
- SQL Server keeps its cross-database browsing locally (`sys.databases`, `[db].dbo.[table]`; the kernel has no cross-database catalog yet), and its index map now labels primary keys, which previously showed as UNIQUE
- Oracle's current namespace is the login user; the old service-name answer never matched a schema in the listing
- Redis connection, scanning, raw commands, and value decoding go through the kernel KVDriver, including its type-enriched scan loop
- Quoter/Escaper optional interfaces to eliminate hardcoded MySQL checks
- Backend interface with capability pattern (Namespaced, TimeRanger, Paged, TableTargeter)
- Inline-prompt SQL builders use TableTargeter interface

## [0.1.0] - 2026-10-13

### Added
- Initial release
- Multi-engine database TUI
- Cell editing with primary-key-based updates
- Inline select/filter/order prompts
- Multi-pane navigation
- Theme system
- Config file persistence
