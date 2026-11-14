// sqltui-plugin-postgres: PostgreSQL support as a process plugin (波 100
// sqltui 进程插件化). The TUI's default build ships MySQL + SQLite; this
// executable is discovered beside it and spawned per connection.
//
// The whole driver logic is the existing postgresbe package — the plugin is
// just a thin process shell over it.

package main

import (
	"context"
	"time"

	"github.com/LinPr/sqltui/internal/db/pluginbe"
	"github.com/LinPr/sqltui/internal/db/pluginsdk"
	"github.com/LinPr/sqltui/internal/db/postgresbe"
)

const pluginKind = "postgres"
const pluginName = "PostgreSQL (plugin)"

type source struct {
	*postgresbe.Backend
}

func (s *source) Open(spec pluginbe.ConnSpec) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	be, err := postgresbe.Connect(ctx, postgresbe.Config{
		UserName: spec.User,
		Password: spec.Password,
		Host:     spec.Host,
		Port:     spec.Port,
		DbName:   spec.Database,
		SslMode:  spec.SSLMode,
	})
	if err != nil {
		return err
	}
	s.Backend = be
	return nil
}

func main() {
	pluginsdk.Main(func() pluginsdk.Source { return &source{} }, pluginKind, pluginName)
}
