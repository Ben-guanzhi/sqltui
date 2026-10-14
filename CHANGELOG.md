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
