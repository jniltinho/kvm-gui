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

func addApplianceTools(s *mcp.Server) {
	no := false
	mcp.AddTool(s, &mcp.Tool{Name: "vm_import_ova", Annotations: &mcp.ToolAnnotations{DestructiveHint: &no},
		Description: "Import an OVF appliance (.ova or .ovf, as VirtualBox exports) as a new Linux, Windows or Other VM, like `vm import-ova`. " +
			"Without confirm it only returns the VM it would create and the notes on what is not kept; show them to the user, then call it with confirm: true. " +
			"macOS appliances are refused. The VM is not started."}, vmImportOVA)
}
