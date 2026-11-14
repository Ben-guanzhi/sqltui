// sqltui-plugin-fake: a protocol-level test plugin (no database). It answers
// every Backend method with fixed data so the host's Dial/call/Close path can
// be driven end to end in pluginbe tests without a server.

package main

import (
	"fmt"

	"github.com/LinPr/sqltui/internal/data"
	"github.com/LinPr/sqltui/internal/db"
	"github.com/LinPr/sqltui/internal/db/pluginbe"
	"github.com/LinPr/sqltui/internal/db/pluginsdk"
)

type fake struct{ spec pluginbe.ConnSpec }

func (f *fake) Open(spec pluginbe.ConnSpec) error { f.spec = spec; return nil }
func (f *fake) Kind() string                      { return "fake" }
func (f *fake) Title() string                     { return "fake://" + f.spec.Host }
func (f *fake) Run(stmt string) (db.Result, error) {
	return db.Result{Frame: &data.Frame{Columns: []data.Column{
		data.NewColumn("stmt", data.TypeString, []any{stmt}),
		data.NewColumn("n", data.TypeInt, []any{int64(42)}),
	}}}, nil
}
func (f *fake) Namespaces() ([]string, error) { return []string{"main"}, nil }
func (f *fake) Tables(ns string) ([]string, error) {
	return []string{"t1", "t2"}, nil
}
func (f *fake) FetchTable(ns, table string, limit int) (*data.Frame, error) {
	return &data.Frame{Columns: []data.Column{
		data.NewColumn("id", data.TypeInt, []any{int64(1)}),
	}}, nil
}
func (f *fake) PrimaryKeys(ns, table string) ([]string, error) { return []string{"id"}, nil }
func (f *fake) ColumnsMeta(ns, table string) ([]db.ColumnMeta, error) {
	return []db.ColumnMeta{{Name: "id", DataType: "int", IsNullable: "NO"}}, nil
}
func (f *fake) ColumnIndexTypes(ns, table string) (map[string]string, error) {
	return map[string]string{"id": "PK"}, nil
}
func (f *fake) Close() error { fmt.Println(""); return nil }

func main() {
	pluginsdk.Main(func() pluginsdk.Source { return &fake{} }, "fake", "Fake Engine")
}
