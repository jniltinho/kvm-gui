package cmd

import (
	"fmt"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jniltinho/kvm-gui/internal/core"
)

var ovaImport core.ImportOptions
var ovaDryRun bool

// vm import-ova: an OVF appliance (VirtualBox's .ova) as a new Linux, Windows or Other VM.
var vmImportOVACmd = &cobra.Command{
	Use:   "import-ova <file.ova|file.ovf>",
	Short: "Import an OVF appliance (.ova or .ovf, as VirtualBox exports) as a new VM",
	Long: `Import an OVF appliance as a new Linux, Windows or Other VM: its OS, memory, processors,
firmware, disks (converted to qcow2), optical drives and network adapters. What kvm-gui cannot
keep is listed first; --dry-run shows that and creates nothing. Bridged adapters become NAT
unless --bridge names one of this host's bridges. The VM is not started. macOS appliances are
refused: macOS VMs move between kvm-gui hosts as bundles (vm export --full).`,
	Example: "  kvm-gui vm import-ova ubuntu.ova --dry-run\n  kvm-gui vm import-ova ubuntu.ova --name \"Ubuntu lab\" --bridge br0",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		o := ovaImport
		if o.Folder != "" && !strings.HasPrefix(o.Folder, "~") {
			abs, err := filepath.Abs(o.Folder)
			if err != nil {
				return err
			}
			o.Folder = abs
		}
		pv, err := core.PlanImport(args[0], o)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		c := pv.Config
		fmt.Fprintf(out, "%s: %s (%s), %d MiB, %d vCPUs, %s firmware%s\nfolder: %s\n", c.Name, c.Profile, c.Guest, c.RAMMiB, c.VCPUs,
			strings.ToUpper(c.Firmware), map[bool]string{true: " with Secure Boot"}[c.SecureBoot], pv.Folder)
		for _, d := range pv.Disks {
			fmt.Fprintln(out, "disk:", d)
		}
		for i, n := range c.Networks {
			fmt.Fprintf(out, "adapter %d: %s, %s\n", i+1, n.Mode+map[bool]string{true: " " + n.Bridge}[n.Bridge != ""], n.Model)
		}
		for _, n := range pv.Notes {
			fmt.Fprintln(out, "note:", n)
		}
		if ovaDryRun {
			return nil
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		e, err := core.ImportAppliance(ctx, args[0], o, func(done, total int64) {
			fmt.Fprintf(cmd.ErrOrStderr(), "\rimporting: %.0f%%", float64(done)*100/float64(max(total, 1)))
		})
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "imported %s (%s), folder %s, SSH port %d\n", e.DisplayName(), e.ID, e.Config.Workspace, e.Config.SSHPort)
		return nil
	},
}

func init() {
	f := vmImportOVACmd.Flags()
	f.StringVar(&ovaImport.Name, "name", "", "the VM's name (default: the appliance's)")
	f.StringVar(&ovaImport.Folder, "folder", "", "the VM's folder (default: <Workspace>/<name>)")
	f.StringVar(&ovaImport.Bridge, "bridge", "", "a host bridge for the appliance's bridged adapters (default: NAT)")
	f.BoolVar(&ovaDryRun, "dry-run", false, "show the VM it would create and what is not kept, and create nothing")
	vmCmd.AddCommand(vmImportOVACmd)
}
