package velero

import "github.com/spf13/cobra"

func newCacheLifecycleCommand(name string, execute func(string, string) error) *cobra.Command {
	return &cobra.Command{
		Use:    name + " DIRECTORY ISOLATION_ID",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, arguments []string) error {
			return execute(arguments[0], arguments[1])
		},
	}
}
