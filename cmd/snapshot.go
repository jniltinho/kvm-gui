package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jniltinho/kvm-gui/internal/core"
)

var (
	snapDescription string
	snapYes         bool
	cloneFolder     string
	cloneNewTPM     bool
	cloneKeepMACs   bool
)

var snapshotCmd = &cobra.Command{Use: "snapshot", Short: "List, take, restore and delete snapshots of a powered-off VM"}

// vmByName is the VM named name (or id).
func vmByName(name string) (core.VMEntry, error) {
	vms, err := core.ListVMs()
	if err != nil {
		return core.VMEntry{}, err
	}
	e, ok := core.FindVM(vms, name)
	if !ok {
		return e, fmt.Errorf("no VM named %q (see `vm list`)", name)
	}
	return recovered(e)
}

var snapshotListCmd = &cobra.Command{
	Use:   "list <vm>",
	Short: "Print the VM's snapshot tree with dates and where the current state is",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := vmByName(args[0])
		if err != nil {
			return err
		}
		snaps, err := core.ListSnapshots(e)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		children := map[string][]core.Snapshot{}
		for _, s := range snaps {
			children[s.ParentID] = append(children[s.ParentID], s)
		}
		var walk func(parent string, depth int)
		walk = func(parent string, depth int) {
			for _, s := range children[parent] {
				line := fmt.Sprintf("%s%s  (%s)", strings.Repeat("  ", depth), s.Name, s.CreatedAt)
				if s.Description != "" {
					line += "  " + s.Description
				}
				fmt.Fprintln(out, line)
				if s.ID == e.Config.CurrentSnapshot {
					fmt.Fprintln(out, strings.Repeat("  ", depth+1)+"* Current state")
				}
				walk(s.ID, depth+1)
			}
		}
		if len(snaps) == 0 || e.Config.CurrentSnapshot == "" {
			fmt.Fprintln(out, "* Current state")
		}
		walk("", 0)
		return nil
	},
}

var snapshotTakeCmd = &cobra.Command{
	Use:   "take <vm> <name>",
	Short: "Take a snapshot of a powered-off VM",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := vmByName(args[0])
		if err != nil {
			return err
		}
		s, err := core.TakeSnapshot(cmd.Context(), e, args[1], snapDescription)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "took snapshot %s of %s\n", s.Name, e.DisplayName())
		return nil
	},
}

var snapshotRestoreCmd = &cobra.Command{
	Use:   "restore <vm> <snapshot>",
	Short: "Return a powered-off VM to a snapshot: its current state is lost unless saved as a snapshot first",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !snapYes {
			return errors.New("restoring loses the VM's current state: take a snapshot first if you need it, then pass --yes")
		}
		e, err := vmByName(args[0])
		if err != nil {
			return err
		}
		if err := core.RestoreSnapshot(cmd.Context(), e, args[1]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "restored %s to %s\n", e.DisplayName(), args[1])
		return nil
	},
}

var snapshotDeleteCmd = &cobra.Command{
	Use:   "delete <vm> <snapshot>",
	Short: "Delete a snapshot; its children move under its parent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !snapYes {
			return errors.New("deleting a snapshot cannot be undone: pass --yes")
		}
		e, err := vmByName(args[0])
		if err != nil {
			return err
		}
		if err := core.DeleteSnapshot(cmd.Context(), e, args[1]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "deleted snapshot %s of %s\n", args[1], e.DisplayName())
		return nil
	},
}

var vmCloneCmd = &cobra.Command{
	Use:   "clone <vm> <new name>",
	Short: "Make a full, independent copy of a powered-off VM (disks, UEFI variables, TPM; new MACs and SSH port)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, err := vmByName(args[0])
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		n, err := core.CloneVM(cmd.Context(), e, args[1], core.CloneOptions{Folder: cloneFolder, NewTPM: cloneNewTPM, KeepMACs: cloneKeepMACs},
			func(s string) { fmt.Fprintln(out, "clone:", s) })
		if err != nil {
			return err
		}
		dir, _ := n.Config.WorkspaceDir()
		fmt.Fprintf(out, "cloned %s as %s (%s), folder %s, SSH port %d\n", e.DisplayName(), n.DisplayName(), n.ID, dir, n.Config.SSHPort)
		return nil
	},
}

func init() {
	snapshotTakeCmd.Flags().StringVar(&snapDescription, "description", "", "a note shown with the snapshot")
	for _, c := range []*cobra.Command{snapshotRestoreCmd, snapshotDeleteCmd} {
		c.Flags().BoolVar(&snapYes, "yes", false, "confirm")
	}
	vmCloneCmd.Flags().StringVar(&cloneFolder, "folder", "", "the clone's folder (default <Workspace>/<new name>)")
	vmCloneCmd.Flags().BoolVar(&cloneNewTPM, "new-tpm", false, "a fresh TPM instead of a copy (BitLocker then asks for its recovery key)")
	vmCloneCmd.Flags().BoolVar(&cloneKeepMACs, "keep-macs", false, "keep the network adapters' MAC addresses")
	snapshotCmd.AddCommand(snapshotListCmd, snapshotTakeCmd, snapshotRestoreCmd, snapshotDeleteCmd)
	vmCmd.AddCommand(vmCloneCmd)
	rootCmd.AddCommand(snapshotCmd)
}
