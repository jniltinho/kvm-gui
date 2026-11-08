package gui

import (
	"sync"

	"github.com/jniltinho/kvm-gui/internal/core"
)

// App is the service the page calls (frontend/src/api.ts). Methods run on Wails' worker
// goroutines; they only read and write through internal/core.
type App struct {
	version string
	ready   func() // set by --self-test: quits once the page has loaded the VM list

	mu sync.Mutex
}

// VM is one row of the Manager's VM list.
type VM struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	State     string `json:"state"`
	Graphics  string `json:"graphics"`
	MemoryMiB int    `json:"memoryMiB"`
	CPUs      int    `json:"cpus"`
}

// Version is the app's version, for the title and About.
func (a *App) Version() string { return a.version }

// Theme is the skin to draw: KVM_GUI_THEME or MKG_THEME (or settings.yaml), else the saved
// preference, else "system"; an unknown id is "system".
func (a *App) Theme() string {
	for _, t := range []string{core.Setting("KVM_GUI_THEME"), core.Setting("MKG_THEME"), core.Preference("theme")} {
		if t != "" {
			return core.ThemeOrSystem(t)
		}
	}
	return "system"
}

// VMs lists the VMs with their state, a running QEMU found through /proc included.
func (a *App) VMs() ([]VM, error) {
	entries, err := core.ListVMs()
	if err != nil {
		return nil, err
	}
	out := make([]VM, 0, len(entries))
	for _, e := range entries {
		state := "Powered Off"
		if _, running := core.FindRunning(e.Config); running {
			state = "Running"
		}
		out = append(out, VM{ID: e.ID, Name: e.Config.Name, Version: e.Config.Version, State: state,
			Graphics: e.Config.Graphics, MemoryMiB: e.Config.RAMMiB, CPUs: e.Config.VCPUs})
	}
	return out, nil
}

// Ready is called by the page once it has drawn the VM list; --self-test then quits.
func (a *App) Ready() {
	a.mu.Lock()
	ready := a.ready
	a.mu.Unlock()
	if ready != nil {
		ready()
	}
}
