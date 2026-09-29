package dbmode

import (
	"fmt"
	"strings"

	"github.com/LinPr/sqltui/internal/ui"
)

// openTableFactory backs the "opentable" command dispatched by the schema
// browser (arg is "ns\ttable"). It shows a small loading overlay while the
// rows are fetched asynchronously.
func openTableFactory(ctx ui.AppContext, arg string) (ui.Overlay, error) {
	be := ctx.Backend()
	if be == nil {
		return nil, fmt.Errorf("opentable: no live database connection")
	}
	ns, table := splitTableArg(arg)
	if strings.TrimSpace(table) == "" {
		return nil, fmt.Errorf("opentable: no table name")
	}
	return ui.NewTableLoader(ctx, be, ns, table, false), nil
}
