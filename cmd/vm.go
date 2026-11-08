package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jniltinho/kvm-gui/internal/core"
)

var (
	vmDisplay string
	vmVerbose bool
	vmBootLog bool
	// vmKextsSnapshot is the MCP server's kexts snapshot (JSON), so its start uses what it downloaded.
	vmKextsSnapshot string
	// vmStarter is who started the run, recorded in run.json: cli, or mcp from the MCP server.
	vmStarter string
	// vmGUIRun and vmEmbedX11: the GUI's detached start; only it embeds Reims vGPU's X11 window.
	vmGUIRun, vmEmbedX11 bool
)

var vmCmd = &cobra.Command{Use: "vm", Short: "Run the macOS VM"}

var vmStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the VM and wait until it stops",
	Long: `Starts the VM in the foreground and returns when QEMU exits.
Ctrl+C asks macOS to shut down (ACPI); a second Ctrl+C forces the stop.
With display=app there is no window: the screen is on the printed private VNC socket.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		starter, err := startStarter(cmd)
		if err != nil {
			return err
		}
		e, err := currentVM()
		if err != nil {
			return err
		}
		c := e.Config
		if vmDisplay != "" {
			c.Display = vmDisplay
		}
		c.VerboseBoot = vmBootLog
		out := cmd.OutOrStdout()
		if c.Backend != core.BackendReimsCtl { // reims-ctl VMs boot reims-ctl's OpenCore: no kexts of the app
			var ks core.ExtraKexts
			if vmKextsSnapshot != "" { // the MCP server's start, which already downloaded them
				err = json.Unmarshal([]byte(vmKextsSnapshot), &ks)
			} else {
				ks, err = core.LoadExtraKexts()
			}
			if err != nil {
				return err
			}
			if !ks.Ready() {
				fmt.Fprintln(out, "vm: downloading the kexts listed in", core.SettingsFile())
				if err := core.DownloadExtraKexts(cmd.Context(), ks); err != nil {
					return err
				}
			}
			if c.ReimsKext() && !core.ReimsKextReady() {
				fmt.Fprintln(out, "vm: downloading ReimsVgpu.kext (Reims vGPU on Tahoe)")
				if err := core.DownloadReimsKext(cmd.Context()); err != nil {
					return err
				}
			}
			c.Extras = &ks
		}
		stopped := make(chan string, 1)
		vm := &core.VM{
			ID: e.ID, Starter: starter, EmbedX11: vmEmbedX11,
			OnState: func(s core.VMState, msg string) {
				if msg != "" {
					fmt.Fprintln(out, "vm:", msg)
				}
				if s == core.Stopped {
					stopped <- msg
				}
			},
		}
		if vmVerbose || starter != core.StarterCLI { // a detached run's output is its start log
			vm.OnLog = func(s string) { fmt.Fprintln(out, s) }
		}
		if err := vm.Start(c); err != nil {
			return err
		}
		fmt.Fprintln(out, "vm: running; Ctrl+C shuts it down")
		if sock := vm.VNCSocket(); sock != "" {
			fmt.Fprintln(out, "vm: screen on VNC socket", sock)
		}
		if line := vm.InstallerCommand(); line != "" {
			fmt.Fprintln(out, "vm: the full installer disc is attached; to install from it, type in the recovery's Terminal:", line)
		}
		sig := make(chan os.Signal, 2)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sig)
		for asked := false; ; {
			select {
			case msg := <-stopped:
				if msg != "" && !asked && !vm.GuestShutDown() {
					return errors.New(msg)
				}
				return nil
			case <-sig:
				if asked {
					fmt.Fprintln(out, "vm: forcing the stop")
					vm.Kill()
					continue
				}
				asked = true
				fmt.Fprintln(out, "vm: asking macOS to shut down (Ctrl+C again to force)")
				if err := vm.Powerdown(); err != nil {
					fmt.Fprintln(out, "vm:", err)
				}
			}
		}
	},
}

func init() {
	vmStartCmd.Flags().StringVar(&vmDisplay, "display", "", "app (VNC socket, no window) or window (QEMU window); default from config")
	vmStartCmd.Flags().BoolVarP(&vmVerbose, "verbose", "v", false, "print the QEMU command line and output")
	vmStartCmd.Flags().StringVar(&vmKextsSnapshot, "kexts-snapshot", "", "")
	vmStartCmd.Flags().MarkHidden("kexts-snapshot")
	vmStartCmd.Flags().StringVar(&vmStarter, "starter", core.StarterCLI, "")
	vmStartCmd.Flags().MarkHidden("starter")
	vmStartCmd.Flags().BoolVar(&vmGUIRun, "gui-run", false, "")
	vmStartCmd.Flags().MarkHidden("gui-run")
	vmStartCmd.Flags().BoolVar(&vmEmbedX11, "embed-x11", false, "")
	vmStartCmd.Flags().MarkHidden("embed-x11")
	vmStartCmd.Flags().BoolVar(&vmBootLog, "verbose-boot", false, "show macOS's boot log on screen instead of the logo, for this start only (boot-args -v debug=0x100)")
	vmCmd.AddCommand(vmStartCmd)
	rootCmd.AddCommand(vmCmd)
}

// startStarter is who starts this run: cli by default, mcp from the MCP server, gui with
// --gui-run. --embed-x11 belongs to GUI runs only.
func startStarter(cmd *cobra.Command) (string, error) {
	if vmEmbedX11 && !vmGUIRun {
		return "", errors.New("--embed-x11 needs --gui-run")
	}
	if vmGUIRun {
		if cmd.Flags().Changed("starter") {
			return "", errors.New("--gui-run and --starter cannot be used together")
		}
		return core.StarterGUI, nil
	}
	if vmStarter != core.StarterCLI && vmStarter != core.StarterMCP {
		return "", fmt.Errorf("unknown starter %q", vmStarter)
	}
	return vmStarter, nil
}
