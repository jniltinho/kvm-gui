package gui

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/jniltinho/kvm-gui/internal/core"
)

// File › Import Appliance… and Export Appliance… (OpenSpec add-ova-import-export): one appliance
// operation at a time, its progress sent to the page as the "appliance-progress" event
// ({done, total} bytes), cancelled by CancelAppliance.

var appliance struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

// ApplianceProgress is the "appliance-progress" event.
type ApplianceProgress struct {
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

// ApplianceOptions are the import's choices in the preview.
type ApplianceOptions struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Bridge string `json:"bridge"`
}

func (o ApplianceOptions) core() core.ImportOptions {
	return core.ImportOptions{Name: strings.TrimSpace(o.Name), Folder: strings.TrimSpace(o.Folder), Bridge: o.Bridge}
}

// PlanAppliance is the import's preview: the VM it would create, its disks and every note.
func (a *App) PlanAppliance(path string, o ApplianceOptions) (core.ImportPreview, error) {
	return core.PlanImport(path, o.core())
}

// begin starts the one appliance operation; false if another runs.
func (a *App) begin() (context.Context, func(), bool) {
	appliance.mu.Lock()
	defer appliance.mu.Unlock()
	if appliance.cancel != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	appliance.cancel = cancel
	return ctx, func() {
		appliance.mu.Lock()
		appliance.cancel = nil
		appliance.mu.Unlock()
		cancel()
	}, true
}

func (a *App) progress(done, total int64) {
	a.emit("appliance-progress", ApplianceProgress{done, total})
}

// ImportAppliance imports the appliance as previewed; the Result has the new VM's id.
func (a *App) ImportAppliance(path string, o ApplianceOptions) Result {
	ctx, end, ok := a.begin()
	if !ok {
		return failed(errors.New("another appliance is being imported or exported"))
	}
	defer end()
	e, err := core.ImportAppliance(ctx, path, o.core(), a.progress)
	if errors.Is(err, context.Canceled) {
		return Result{Error: "cancelled: nothing was imported"}
	}
	return created(e, err)
}

// ApplianceExportNotes is what an export of the VM leaves behind, shown before it starts.
func (a *App) ApplianceExportNotes(id string) ([]string, error) {
	e, err := entry(id)
	if err != nil {
		return nil, err
	}
	notes := core.ExportNotes(e)
	if notes == nil {
		notes = []string{}
	}
	return notes, nil
}

// ExportAppliance writes the VM as an .ova at path; overwrite and acceptTPM are the user's answers.
func (a *App) ExportAppliance(id, path string, overwrite, acceptTPM bool) Result {
	e, err := entry(id)
	if err != nil {
		return failed(err)
	}
	ctx, end, ok := a.begin()
	if !ok {
		return failed(errors.New("another appliance is being imported or exported"))
	}
	defer end()
	err = core.ExportAppliance(ctx, e, path, core.ExportOptions{Overwrite: overwrite, AcceptTPMLoss: acceptTPM}, a.progress)
	if errors.Is(err, context.Canceled) {
		return Result{Error: "cancelled: nothing was written"}
	}
	if err != nil {
		return failed(err)
	}
	return Result{ID: e.ID}
}

// CancelAppliance stops the running import or export; it removes what it wrote.
func (a *App) CancelAppliance() {
	appliance.mu.Lock()
	defer appliance.mu.Unlock()
	if appliance.cancel != nil {
		appliance.cancel()
	}
}

// PickSaveAppliance asks where to write an .ova, suggesting name.
func (a *App) PickSaveAppliance(name string) (string, error) {
	if a.save == nil {
		return "", errors.New("no window")
	}
	return a.save("Export appliance", core.Slug(name)+".ova", [][2]string{{"Appliances (*.ova)", "*.ova"}})
}
