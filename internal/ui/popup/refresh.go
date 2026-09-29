// Refresh overlay: re-fetches the table the active pane shows and swaps it
// into the pane's base frame in place (no new tab). Registered under the
// command name "refreshtable" and dispatched by the ActRefresh key binding in
// database mode.
package popup

import (
	"fmt"

	"github.com/LinPr/sqltui/internal/ui"
)

func init() {
	RegisterCommand(Command{Name: "refreshtable", Description: "refresh the current table"},
		func(ctx ui.AppContext, arg string) (ui.Overlay, error) {
			be := ctx.Backend()
			if be == nil {
				return nil, fmt.Errorf("refreshtable: no live database connection")
			}
			return ui.NewTableLoader(ctx, be, ctx.CurrentTableNamespace(), ctx.BaseCrumb(), true), nil
		})
}
