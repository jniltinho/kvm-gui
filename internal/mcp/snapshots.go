package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jniltinho/kvm-gui/internal/core"
)

type snapshotListOut struct {
	Snapshots []core.Snapshot `json:"snapshots"`
	Current   string          `json:"current_snapshot" jsonschema:"id of the snapshot the current state is under; empty for none"`
}

func snapshotList(_ context.Context, _ *mcp.CallToolRequest, in vmArg) (*mcp.CallToolResult, snapshotListOut, error) {
	e, err := findVM(in.VM)
	if err != nil {
		return nil, snapshotListOut{}, err
	}
	snaps, err := core.ListSnapshots(e)
	if err != nil {
		return nil, snapshotListOut{}, err
	}
	e, _ = findVM(e.ID)
	if snaps == nil {
		snaps = []core.Snapshot{}
	}
	return nil, snapshotListOut{snaps, e.Config.CurrentSnapshot}, nil
}

type snapshotTakeIn struct {
	VM          string `json:"vm,omitempty" jsonschema:"VM name or id; default: the VM selected in the app"`
	Name        string `json:"name" jsonschema:"the snapshot's name"`
	Description string `json:"description,omitempty"`
}

func snapshotTake(ctx context.Context, _ *mcp.CallToolRequest, in snapshotTakeIn) (*mcp.CallToolResult, core.Snapshot, error) {
	e, err := findVM(in.VM)
	if err != nil {
		return nil, core.Snapshot{}, err
	}
	s, err := core.TakeSnapshot(ctx, e, in.Name, in.Description)
	return nil, s, err
}

type snapshotIn struct {
	VM       string `json:"vm,omitempty" jsonschema:"VM name or id; default: the VM selected in the app"`
	Snapshot string `json:"snapshot" jsonschema:"snapshot name or id"`
	Confirm  bool   `json:"confirm,omitempty" jsonschema:"must be true: the user agreed (restore loses the current state; delete cannot be undone)"`
}

func snapshotRestore(ctx context.Context, _ *mcp.CallToolRequest, in snapshotIn) (*mcp.CallToolResult, any, error) {
	if !in.Confirm {
		return nil, nil, errors.New("restoring loses the VM's current state: take a snapshot first if it is needed, ask the user, then pass confirm: true")
	}
	e, err := findVM(in.VM)
	if err != nil {
		return nil, nil, err
	}
	if err := core.RestoreSnapshot(ctx, e, in.Snapshot); err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("Restored %s to %s.", e.DisplayName(), in.Snapshot)), nil, nil
}

func snapshotDelete(ctx context.Context, _ *mcp.CallToolRequest, in snapshotIn) (*mcp.CallToolResult, any, error) {
	if !in.Confirm {
		return nil, nil, errors.New("deleting a snapshot cannot be undone: ask the user, then pass confirm: true")
	}
	e, err := findVM(in.VM)
	if err != nil {
		return nil, nil, err
	}
	if err := core.DeleteSnapshot(ctx, e, in.Snapshot); err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("Deleted snapshot %s of %s.", in.Snapshot, e.DisplayName())), nil, nil
}

type cloneIn struct {
	VM       string `json:"vm,omitempty" jsonschema:"VM name or id; default: the VM selected in the app"`
	Name     string `json:"name" jsonschema:"the clone's name"`
	Folder   string `json:"folder,omitempty" jsonschema:"the clone's folder; default <Workspace>/<name>"`
	NewTPM   bool   `json:"new_tpm,omitempty" jsonschema:"a fresh TPM instead of a copy (BitLocker then asks for its recovery key)"`
	KeepMACs bool   `json:"keep_macs,omitempty" jsonschema:"keep the network adapters' MAC addresses"`
}

func vmClone(ctx context.Context, _ *mcp.CallToolRequest, in cloneIn) (*mcp.CallToolResult, vmConfigOut, error) {
	e, err := findVM(in.VM)
	if err != nil {
		return nil, vmConfigOut{}, err
	}
	n, err := core.CloneVM(ctx, e, in.Name, core.CloneOptions{Folder: in.Folder, NewTPM: in.NewTPM, KeepMACs: in.KeepMACs}, nil)
	if err != nil {
		return nil, vmConfigOut{}, err
	}
	return nil, vmConfigOut{ID: n.ID, Config: n.Config, Notice: n.Notice}, nil
}

func addSnapshotTools(s *mcp.Server, readOnly, destructive *mcp.ToolAnnotations) {
	mcp.AddTool(s, &mcp.Tool{Name: "snapshot_list", Annotations: readOnly,
		Description: "List a VM's snapshots (a tree by parent_id) and the snapshot its current state is under."}, snapshotList)
	mcp.AddTool(s, &mcp.Tool{Name: "snapshot_take",
		Description: "Take a snapshot of a powered-off VM: its disks, UEFI variables, TPM state and settings."}, snapshotTake)
	mcp.AddTool(s, &mcp.Tool{Name: "snapshot_restore", Annotations: destructive,
		Description: "Return a powered-off VM to a snapshot; its current state is lost unless saved as a snapshot first. Needs confirm: true; ask the user first."}, snapshotRestore)
	mcp.AddTool(s, &mcp.Tool{Name: "snapshot_delete", Annotations: destructive,
		Description: "Delete a snapshot of a powered-off VM; its children move under its parent. Needs confirm: true; ask the user first."}, snapshotDelete)
	mcp.AddTool(s, &mcp.Tool{Name: "vm_clone",
		Description: "Make a full, independent copy of a powered-off VM: every disk, the UEFI variables and the TPM state, with new MACs and SSH port."}, vmClone)
}
