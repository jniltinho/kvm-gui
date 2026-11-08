package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// licenses is the project's LICENSE and THIRD_PARTY_NOTICES.md, embedded by main.
var licenses string

var licensesCmd = &cobra.Command{
	Use:   "licenses",
	Short: "Print kvm-gui's license and the third-party notices",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprint(cmd.OutOrStdout(), licenses)
		return err
	},
}

func init() { rootCmd.AddCommand(licensesCmd) }
