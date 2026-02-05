package entry

import (
	"context"
	"os"

	"github.com/andrewstucki/migration-experiment/log"
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entry",
		Short: "Entrypoint",
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

			if err := run(ctx); err != nil {
				log.Error(err, "failed to run")
				os.Exit(1)
			}
		},
	}

	return cmd
}

func run(ctx context.Context) error {
	<-ctx.Done()

	return nil
}
