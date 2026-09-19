package cmd

import (
	"github.com/spf13/cobra"

	"github.com/LinPr/sqltui/internal/ui/dbmode"
)

var sqlserverCmd = &cobra.Command{
	Use:   "sqlserver",
	Short: "Connect to a SQL Server",
	Long: `Connect to a live SQL Server.

A connection form opens first, asking for host, port, user, password and
database. It is prefilled from the saved config (~/.config/sqltui/config.yaml)
and ctrl+s stores the values back for next time.

After connecting, the schema browser lists the server's databases and tables:
selecting a table loads its rows into a tab, and :query runs any SQL statement
against the live connection (non-query statements report rows affected).`,
	Example: `  # open the connection form, prefilled from the saved config
  sqltui sqlserver`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return dbmode.Run(dbmode.KindSqlServer)
	},
}

func init() {
	rootCmd.AddCommand(sqlserverCmd)
}
