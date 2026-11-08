package mcp

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jniltinho/kvm-gui/internal/core"
)

type importOVAIn struct {
	Path    string `json:"path" jsonschema:"absolute path of the .ova or .ovf"`
	Name    string `json:"name,omitempty" jsonschema:"the VM's name; default: the appliance's"`
	Folder  string `json:"folder,omitempty" jsonschema:"absolute folder for the VM; default: <Workspace>/<name>"`
	Bridge  string `json:"bridge,omitempty" jsonschema:"a host bridge for the appliance's bridged adapters; default: NAT"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"false (default): only return the VM it would create and what is not kept; true: import it"`
}

type importOVAOut struct {
	Preview core.ImportPreview `json:"preview"`
	ID      string             `json:"id,omitempty" jsonschema:"the new VM's id, when confirm was true"`
}

func vmImportOVA(ctx context.Context, _ *mcp.CallToolRequest, in importOVAIn) (*mcp.CallToolResult, importOVAOut, error) {
	if !filepath.IsAbs(in.Path) || (in.Folder != "" && !filepath.IsAbs(in.Folder)) {
		return nil, importOVAOut{}, errors.New("path and folder must be absolute")
	}
	o := core.ImportOptions{Name: in.Name, Folder: in.Folder, Bridge: in.Bridge}
	pv, err := core.PlanImport(in.Path, o)
	if err != nil {
		return nil, importOVAOut{}, err
	}
	if !in.Confirm {
		return nil, importOVAOut{Preview: pv}, nil
	}
	e, err := core.ImportAppliance(ctx, in.Path, o, nil)
	if err != nil {
		return nil, importOVAOut{}, err
	}
	return nil, importOVAOut{Preview: pv, ID: e.ID}, nil
}

type exportOVAIn struct {
	VM            string `json:"vm,omitempty" jsonschema:"VM name or id; default: the VM selected in the app"`
	Path          string `json:"path" jsonschema:"absolute path of the .ova to write"`
	Overwrite     bool   `json:"overwrite,omitempty" jsonschema:"replace the file if it exists"`
	AcceptTPMLoss bool   `json:"accept_tpm_loss,omitempty" jsonschema:"the VM has a TPM: its sealed secrets (BitLocker) need their recovery key elsewhere; ask the user first"`
}

type exportOVAOut struct {
	Path  string   `json:"path"`
	Notes []string `json:"notes" jsonschema:"what the appliance does not carry"`
}

func vmExportOVA(ctx context.Context, _ *mcp.CallToolRequest, in exportOVAIn) (*mcp.CallToolResult, exportOVAOut, error) {
	if !filepath.IsAbs(in.Path) {
		return nil, exportOVAOut{}, errors.New("path must be absolute")
	}
	e, err := findVM(in.VM)
	if err != nil {
		return nil, exportOVAOut{}, err
	}
	notes := core.ExportNotes(e)
	if err := core.ExportAppliance(ctx, e, in.Path, core.ExportOptions{Overwrite: in.Overwrite, AcceptTPMLoss: in.AcceptTPMLoss}, nil); err != nil {
		return nil, exportOVAOut{}, err
	}
	if notes == nil {
		notes = []string{}
	}
	return nil, exportOVAOut{Path: in.Path, Notes: notes}, nil
}

func addApplianceTools(s *mcp.Server) {
	no := false
	mcp.AddTool(s, &mcp.Tool{Name: "vm_import_ova", Annotations: &mcp.ToolAnnotations{DestructiveHint: &no},
		Description: "Import an OVF appliance (.ova or .ovf, as VirtualBox exports) as a new Linux, Windows or Other VM, like `vm import-ova`. " +
			"Without confirm it only returns the VM it would create and the notes on what is not kept; show them to the user, then call it with confirm: true. " +
			"macOS appliances are refused. The VM is not started."}, vmImportOVA)
	yes := true
	mcp.AddTool(s, &mcp.Tool{Name: "vm_export_ova", Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes},
		Description: "Export a powered-off Linux, Windows or Other VM as an .ova that VirtualBox imports, like `vm export-ova`. " +
			"An existing file is kept unless overwrite is true (then destructive). Snapshots, TPM state and UEFI variables are not included; " +
			"a VM with a TPM needs accept_tpm_loss after the user agreed. Returns the notes."}, vmExportOVA)
}
