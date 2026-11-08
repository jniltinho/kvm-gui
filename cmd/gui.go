package cmd

import (
	"github.com/spf13/cobra"

	"github.com/jniltinho/kvm-gui/internal/gui"
)

var guiOpts gui.Options

var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "Open the desktop GUI (the default without a command)",
	RunE:  func(*cobra.Command, []string) error { return runGUI() },
}

func runGUI() error {
	guiOpts.Version = version
	return gui.Run(guiOpts)
}

func init() {
	for _, c := range []*cobra.Command{rootCmd, guiCmd} {
		c.Flags().BoolVar(&guiOpts.SafeGPU, "safe-gpu", false, "start the window with GPU acceleration off (or KVM_GUI_SAFE_GPU=1)")
		c.Flags().BoolVar(&guiOpts.SelfTest, "self-test", false, "open the Manager, wait until it has loaded the VM list, and exit 0")
	}
	rootCmd.AddCommand(guiCmd)
}
