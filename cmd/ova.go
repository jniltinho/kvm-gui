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
		e, err := core.ImportAppliance(ctx, args[0], o, percent(cmd, "importing"))
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

var ovaExport core.ExportOptions

// vm export-ova: a powered-off Linux, Windows or Other VM as an .ova VirtualBox imports.
var vmExportOVACmd = &cobra.Command{
	Use:   "export-ova <vm> <file.ova>",
	Short: "Export a powered-off VM as an OVF appliance (.ova) that VirtualBox imports",
	Long: `Export a powered-off Linux, Windows or Other VM as an .ova: an OVF 1.0 descriptor, its disks
as VMDKs and a SHA-256 manifest. Snapshots, TPM state and UEFI variables are not included; a VM
with a TPM needs --accept-tpm-loss (have the BitLocker recovery key). An existing file is kept
unless --force. macOS VMs are refused (export them as bundles: vm export --full).`,
	Example: "  kvm-gui vm export-ova \"Ubuntu lab\" ~/ubuntu-lab.ova",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		vms, err := core.ListVMs()
		if err != nil {
			return err
		}
		e, ok := core.FindVM(vms, args[0])
		if !ok {
			return fmt.Errorf("no VM named %q (see `vm list`)", args[0])
		}
		dst, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		for _, n := range core.ExportNotes(e) {
			fmt.Fprintln(cmd.OutOrStdout(), "note:", n)
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		err = core.ExportAppliance(ctx, e, dst, ovaExport, percent(cmd, "exporting"))
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "exported", e.DisplayName(), "to", dst)
		return nil
	},
}

func init() {
	f := vmExportOVACmd.Flags()
	f.BoolVar(&ovaExport.Overwrite, "force", false, "replace the file if it exists")
	f.BoolVar(&ovaExport.AcceptTPMLoss, "accept-tpm-loss", false, "export a VM with a TPM, knowing its sealed secrets (BitLocker) need their recovery key elsewhere")
	vmCmd.AddCommand(vmExportOVACmd)
}

// percent prints "label: N%" on stderr each time the whole percentage changes.
func percent(cmd *cobra.Command, label string) func(done, total int64) {
	last := -1
	return func(done, total int64) {
		if p := int(done * 100 / max(total, 1)); p != last {
			last = p
			fmt.Fprintf(cmd.ErrOrStderr(), "\r%s: %d%%", label, p)
		}
	}
}
