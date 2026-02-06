package entry

import (
	"os"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/andrewstucki/migration-experiment/log"
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entry",
		Short: "Entrypoint",
		Run: func(cmd *cobra.Command, args []string) {
			if err := run(); err != nil {
				log.Error(err, "failed to run")
				os.Exit(1)
			}
		},
	}

	return cmd
}

func run() error {
	<-ctrl.SetupSignalHandler().Done()

	return nil
}
