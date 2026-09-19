package cmd

import (
	"github.com/spf13/cobra"

	"github.com/LinPr/sqltui/internal/ui/dbmode"
)

var openobserveCmd = &cobra.Command{
	Use:     "openobserve",
	Aliases: []string{"oo"},
	Short:   "Connect to an OpenObserve server",
	Long: `Connect to a live OpenObserve server.

A connection form opens first, asking for host, port, user, password and
org. It is prefilled from the saved config (~/.config/sqltui/config.yaml)
and ctrl+s stores the values back for next time.

After connecting, the schema browser lists the available streams.`,
	Example: `  # open the connection form, prefilled from the saved config
  sqltui openobserve`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return dbmode.Run(dbmode.KindOpenObserve)
	},
}

func init() {
	rootCmd.AddCommand(openobserveCmd)
}
