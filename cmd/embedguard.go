package cmd

import (
	"github.com/spf13/cobra"

	"github.com/jniltinho/kvm-gui/internal/embed"
)

// The GUI starts one per embedded VM window: it gives the window back to the desktop if the GUI
// dies without doing so (internal/embed/guard.go).
var embedGuardCmd = &cobra.Command{
	Use:    "embed-guard <window> <container>",
	Short:  "Keep an embedded VM window when the GUI dies (started by the GUI)",
	Hidden: true,
	Args:   cobra.ExactArgs(2),
	RunE:   func(_ *cobra.Command, args []string) error { return embed.GuardMain(args) },
}

func init() { rootCmd.AddCommand(embedGuardCmd) }
