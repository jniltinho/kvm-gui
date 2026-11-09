package gui

import (
	"errors"
	"github.com/wailsapp/wails/v3/pkg/application"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"sync"

	"github.com/jniltinho/kvm-gui/internal/core"
)

// App is the service the page calls (frontend/src/api.ts). Methods run on Wails' worker
// goroutines; they only read and write through internal/core.
type App struct {
	version string
	ready   func() // set by --self-test: quits once the page has loaded the VM list
	// Set by Run once the window exists: the interface scale and the native file choosers.
	zoom func(float64)
	pick func(title string, folder bool, filters [][2]string) (string, error)
	save func(title, name string, filters [][2]string) (string, error)
	send func(name string, data any) // an event to the page
	show func(id, title string)      // opens or focuses the VM's window
	// vmWindow is id's open VM window, nil when it is closed.
	vmWindow func(id string) *application.WebviewWindow
	// mainWindow is the Manager's window.
	mainWindow func() *application.WebviewWindow

	screen *screenServer // the VMs' screens for the page; nil when it could not start

	mu sync.Mutex
}

// emit sends the page an event; nothing before the window exists.
func (a *App) emit(name string, data any) {
	if a.send != nil {
		a.send(name, data)
	}
}

// Version is the app's version, for the title and About.
func (a *App) Version() string { return a.version }

// Theme is the skin to draw: KVM_GUI_THEME or MKG_THEME (or settings.yaml), else the saved
// preference, else "system"; an unknown id is "system".
func (a *App) Theme() string {
	for _, t := range []string{core.Setting("MKG_THEME"), core.Preference("theme")} { // MKG_THEME reads KVM_GUI_THEME first
		if t != "" {
			return core.ThemeOrSystem(t)
		}
	}
	return "system"
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

// Prefs are File › Preferences.
type Prefs struct {
	Workspace string `json:"workspace"`
	Theme     string `json:"theme"` // the saved choice; an environment override wins (Theme)
	Scale     string `json:"scale"` // interface scale in percent: "100", "125", "150" or "200"
	HostKey   string `json:"hostKey"`
}

// hostKey captures and releases the keyboard of a VM screen (frontend/src/viewer.ts HOST_KEY); Ctrl+Alt+F
// toggles full screen.
const hostKey = "Right Ctrl"

// scales are the interface scales offered, in percent.
var scales = []string{"100", "125", "150", "200"}

func scaleOf(s string) string {
	for _, v := range scales {
		if v == s {
			return s
		}
	}
	return "100"
}

func zoomOf(s string) float64 {
	f, _ := strconv.ParseFloat(scaleOf(s), 64)
	return f / 100
}

// Preferences returns the saved preferences.
func (a *App) Preferences() Prefs {
	return Prefs{Workspace: core.WorkspaceRoot(), Theme: core.ThemeOrSystem(core.Preference("theme")),
		Scale: scaleOf(core.Preference("ui_scale")), HostKey: hostKey}
}

// SetTheme saves the skin; the page applies it at once.
func (a *App) SetTheme(id string) error { return core.SetPreference("theme", core.ThemeOrSystem(id)) }

// SetScale saves the interface scale and redraws the window at it.
func (a *App) SetScale(percent string) error {
	percent = scaleOf(percent)
	if err := core.SetPreference("ui_scale", percent); err != nil {
		return err
	}
	if a.zoom != nil {
		a.zoom(zoomOf(percent))
	}
	return nil
}

// SetWorkspace saves the folder new VMs go into; existing VMs stay where they are.
func (a *App) SetWorkspace(root string) error { return core.SetWorkspaceRoot(root) }

// PickFile asks for a file with the native chooser; "" when cancelled. kind picks the filter:
// "iso", "disk", "json" or "" (any file).
func (a *App) PickFile(title, kind string) (string, error) {
	if a.pick == nil {
		return "", errors.New("no window")
	}
	var f [][2]string
	switch kind {
	case "iso":
		f = [][2]string{{"Optical disc images (*.iso)", "*.iso;*.ISO"}}
	case "disk":
		f = [][2]string{{"Disk images (*.qcow2, *.img, *.raw)", "*.qcow2;*.img;*.raw"}}
	case "json":
		f = [][2]string{{"VM settings (*.json)", "*.json"}}
	case "ova":
		f = [][2]string{{"Appliances (*.ova, *.ovf)", "*.ova;*.OVA;*.ovf;*.OVF"}}
	}
	return a.pick(title, false, f)
}

// PickFolder asks for a folder with the native chooser; "" when cancelled.
func (a *App) PickFolder(title string) (string, error) {
	if a.pick == nil {
		return "", errors.New("no window")
	}
	return a.pick(title, true, nil)
}

// Choice is an id and its label.
type Choice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Profile is an OS profile's defaults, for the New VM wizard.
type Profile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Guest      string `json:"guest"`
	Firmware   string `json:"firmware"`
	SecureBoot bool   `json:"secureBoot"`
	TPM        bool   `json:"tpm"`
	DiskBus    string `json:"diskBus"`
	NIC        string `json:"nic"`
	Graphics   string `json:"graphics"`
	RAMMiB     int    `json:"ramMiB"`
	MinRAMMiB  int    `json:"minRamMiB"`
	DiskGiB    int    `json:"diskGiB"`
}

// Catalog is what the wizard and Settings offer on this host.
type Catalog struct {
	Versions         []Choice  `json:"versions"`
	Profiles         []Profile `json:"profiles"`
	NICModels        []string  `json:"nicModels"`
	GuestNICModels   []string  `json:"guestNicModels"`
	Resolutions      []string  `json:"resolutions"`      // macOS (Reims vGPU)
	GuestResolutions []string  `json:"guestResolutions"` // Linux, Windows and Other
	Bridges          []string  `json:"bridges"`
	HostMemMiB       int       `json:"hostMemMiB"`
	HostThreads      int       `json:"hostThreads"`
	// Recommended CPU and RAM for a macOS VM on this host (core.Recommended).
	RecRAMMiB    int    `json:"recRamMiB"`
	RecVCPUs     int    `json:"recVcpus"`
	MinMacRAMMiB int    `json:"minMacRamMiB"`
	MacDiskGiB   int    `json:"macDiskGiB"`
	Workspace    string `json:"workspace"`
	// ReimsMissing says why Reims vGPU cannot run here ("" when its QEMU is installed).
	ReimsMissing string `json:"reimsMissing"`
	// Render3D says whether 3D and Vulkan guests can run here; RenderGPUs are the GPUs they can
	// draw on, AutoRenderGPU the address of the automatic choice ("" with none).
	Render3D      core.Guest3D     `json:"render3D"`
	RenderGPUs    []core.RenderGPU `json:"renderGPUs"`
	AutoRenderGPU string           `json:"autoRenderGPU"`
}

// Catalog lists the versions, profiles and host facts the forms need.
func (a *App) Catalog() Catalog {
	h := core.SystemHost()
	mem := h.MemTotalMiB()
	rec := core.Recommended(core.Defaults(), runtime.NumCPU(), mem)
	c := Catalog{NICModels: core.NICModels, GuestNICModels: core.GuestNICModels, Resolutions: core.Resolutions, GuestResolutions: core.GuestResolutions,
		Bridges: core.HostBridges(), HostMemMiB: mem, HostThreads: runtime.NumCPU(),
		RecRAMMiB: rec.RAMMiB, RecVCPUs: rec.VCPUs, MinMacRAMMiB: core.MinRAMMiB, MacDiskGiB: core.DefaultDiskGiB,
		Workspace: core.WorkspaceRoot()}
	for _, v := range core.Versions {
		c.Versions = append(c.Versions, Choice{v.Short, v.Name})
	}
	for _, p := range core.Profiles {
		c.Profiles = append(c.Profiles, Profile{p.ID, p.Name, p.Guest, p.Firmware, p.SecureBoot, p.TPM, p.DiskBus, p.NIC,
			p.Graphics, p.RAMMiB, p.MinRAMMiB, p.DiskGiB})
	}
	if _, err := core.ReimsInstalled(); err != nil {
		c.ReimsMissing = err.Error()
	}
	c.Render3D = h.Guest3DSupport(core.SystemQEMU())
	c.RenderGPUs = h.RenderGPUs()
	if c.RenderGPUs == nil {
		c.RenderGPUs = []core.RenderGPU{}
	}
	if g, ok := core.AutoRenderGPU(c.RenderGPUs); ok {
		c.AutoRenderGPU = g.Addr
	}
	return c
}

// A11yAudit tells the page to report controls without an accessible name (KVM_GUI_A11Y_AUDIT=1): the
// accessibility check of the screens under Xvfb, where no AT-SPI bus runs.
func (a *App) A11yAudit() bool { return os.Getenv("KVM_GUI_A11Y_AUDIT") == "1" }

// A11yReport logs what the page's audit found on the screen it shows.
func (a *App) A11yReport(screen string, controls int, unnamed []string) {
	slog.Info("a11y", "screen", screen, "controls", controls, "unnamed", unnamed)
}

// Detected is an ISO's volume label and the profile it matches.
type Detected struct {
	Label   string `json:"label"`
	Profile string `json:"profile"` // "" when the label matches no known system
	Guest   string `json:"guest"`
	Name    string `json:"name"` // the name to suggest
}

// DetectISO reads an installer ISO's label offline and proposes its profile.
func (a *App) DetectISO(path string) Detected {
	p, label, ok := core.DetectProfile(path)
	if !ok {
		return Detected{Label: label}
	}
	return Detected{Label: label, Profile: p.ID, Guest: p.Guest, Name: p.DefaultName}
}
