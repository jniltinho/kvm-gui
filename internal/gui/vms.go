package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jniltinho/kvm-gui/internal/core"
)

// VM is one row of the Manager's VM list.
type VM struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Guest     string `json:"guest"` // macos, linux, windows or other
	OS        string `json:"os"`    // "macOS Sequoia (15)", "Ubuntu", …
	Version   string `json:"version"`
	State     string `json:"state"`
	Running   bool   `json:"running"`
	Screen    bool   `json:"screen"` // the run exports its screen to the app (else QEMU's own window)
	Graphics  string `json:"graphics"`
	MemoryMiB int    `json:"memoryMiB"`
	CPUs      int    `json:"cpus"`
}

func guestOf(c core.Config) string {
	if c.MacOS() {
		return core.GuestMacOS
	}
	return c.Guest
}

// osName is the system a VM runs, as the list and Details show it.
func osName(c core.Config) string {
	if c.MacOS() {
		for _, v := range core.Versions {
			if v.Short == c.Version {
				return "macOS " + v.Name
			}
		}
		return strings.TrimSpace("macOS " + c.Version) // a reims-ctl VM may not say which
	}
	if p, ok := core.ProfileByID(c.Profile); ok {
		return p.Name
	}
	return c.Guest
}

func row(e core.VMEntry) VM {
	run, running := core.FindRun(e)
	state := "Powered Off"
	if running {
		state = "Running"
	}
	return VM{ID: e.ID, Name: e.DisplayName(), Guest: guestOf(e.Config), OS: osName(e.Config), Version: e.Config.Version,
		State: state, Running: running, Screen: running && run.VNCSocket != "", Graphics: e.Config.Graphics, MemoryMiB: e.Config.RAMMiB, CPUs: e.Config.VCPUs}
}

// VMs lists the VMs with their state, a running QEMU found through /proc included.
func (a *App) VMs() ([]VM, error) {
	entries, err := core.ListVMs()
	if err != nil {
		return nil, err
	}
	out := make([]VM, 0, len(entries))
	for _, e := range entries {
		out = append(out, row(e))
	}
	return out, nil
}

func entry(id string) (core.VMEntry, error) {
	entries, err := core.ListVMs()
	if err != nil {
		return core.VMEntry{}, err
	}
	for _, e := range entries {
		if e.ID == id {
			return e, nil
		}
	}
	return core.VMEntry{}, fmt.Errorf("VM %s no longer exists", id)
}

// DiskInfo is a hard disk as Details and the Media manager show it.
type DiskInfo struct {
	Path      string `json:"path"`
	Format    string `json:"format"`
	Bus       string `json:"bus"`
	SizeBytes int64  `json:"sizeBytes"` // virtual size, 0 when unknown
	UsedBytes int64  `json:"usedBytes"` // allocated on the host
	Missing   bool   `json:"missing"`
}

func diskInfo(path, format, bus string) DiskInfo {
	st, err := os.Stat(path)
	d := DiskInfo{Path: path, Format: format, Bus: bus, SizeBytes: core.DiskVirtualSize(path),
		UsedBytes: core.Allocated(path), Missing: err != nil}
	if d.SizeBytes == 0 && err == nil {
		d.SizeBytes = st.Size() // raw: the file is the disk (DiskVirtualSize reads qcow2 only)
	}
	return d
}

// disks are the VM's hard disks with absolute paths: mac_hdd_ng.img for macOS VMs (a reims-ctl
// VM's are the ones reims-ctl's command names, in Disks).
func disks(c core.Config) []DiskInfo {
	var out []DiskInfo
	if c.MacOS() && c.Backend != core.BackendReimsCtl {
		if dir, err := c.WorkspaceDir(); err == nil {
			out = append(out, diskInfo(filepath.Join(dir, core.DiskFile), "qcow2", "sata"))
		}
		return out
	}
	for _, d := range c.Disks {
		if p, err := c.DiskPath(d); err == nil {
			out = append(out, diskInfo(p, d.Format, d.Bus))
		}
	}
	return out
}

// VMInfo is the selected VM for Details and Settings.
type VMInfo struct {
	VM
	Config core.Config `json:"config"`
	Disks  []DiskInfo  `json:"disks"`
	// Recovery is the macOS install media (BaseSystem.img) when it is in the folder.
	Recovery string `json:"recovery"`
	// HasVars: the UEFI variables exist, so toggling Secure Boot replaces them (and their boot entries).
	HasVars bool   `json:"hasVars"`
	SSHWarn string `json:"sshWarn"`
}

// VM returns one VM's settings and disks.
func (a *App) VM(id string) (VMInfo, error) {
	e, err := entry(id)
	if err != nil {
		return VMInfo{}, err
	}
	if r, err := core.RefreshReimsCtlVM(context.Background(), e); err == nil {
		e = r // a reims-ctl VM shows what reims-ctl has now; when it cannot answer, its stored settings
	}
	info := VMInfo{VM: row(e), Config: e.Config, Disks: disks(e.Config), SSHWarn: core.SSHPortWarning(e.Config.SSHPort, e.ID)}
	if info.Config.Networks == nil {
		info.Config.Networks = e.Config.Adapters() // the legacy implicit adapter, written out for the forms
	}
	if dir, err := e.Config.WorkspaceDir(); err == nil {
		if p := filepath.Join(dir, core.MediaFile); e.Config.MacOS() && exists(p) {
			info.Recovery = p
		}
		info.HasVars = exists(filepath.Join(dir, core.GuestVarsFile))
	}
	return info, nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Result is the outcome of a form: Field names the invalid field (core.FieldError, "" for none),
// Error the message; ID and Notice are set by a create.
type Result struct {
	ID     string `json:"id,omitempty"`
	Notice string `json:"notice,omitempty"`
	Field  string `json:"field,omitempty"`
	Error  string `json:"error,omitempty"`
}

func failed(err error) Result {
	var fe *core.FieldError
	if errors.As(err, &fe) {
		return Result{Field: fe.Field, Error: fe.Msg}
	}
	return Result{Error: err.Error()}
}

// checkHost refuses settings the host cannot give: more memory than it has.
func checkHost(c core.Config) error {
	if mem := core.SystemHost().MemTotalMiB(); mem > 0 && c.RAMMiB > mem {
		return &core.FieldError{Field: "ram", Msg: fmt.Sprintf("Base memory is more than this host has: use at most %d MiB", mem)}
	}
	return nil
}

// SaveVM validates and stores a VM's settings (Settings › OK). While the VM runs, what cannot
// change then (System, storage, guest type, display adapter) keeps its stored value. resetVars
// replaces the UEFI variables after a Secure Boot change the user confirmed.
func (a *App) SaveVM(id string, c core.Config, resetVars bool) Result {
	e, err := entry(id)
	if err != nil {
		return failed(err)
	}
	if _, running := core.FindRun(e); running {
		o := e.Config
		c.Guest, c.Profile, c.Version, c.Workspace = o.Guest, o.Profile, o.Version, o.Workspace
		c.RAMMiB, c.Cores, c.VCPUs, c.Firmware, c.SecureBoot, c.TPM = o.RAMMiB, o.Cores, o.VCPUs, o.Firmware, o.SecureBoot, o.TPM
		c.Disks, c.Graphics, c.GPU, c.Resolution, c.RootPatch, c.Display = o.Disks, o.Graphics, o.GPU, o.Resolution, o.RootPatch, o.Display
		resetVars = false
	}
	if err := checkHost(c); err != nil {
		return failed(err)
	}
	if err := core.SaveVM(e, c); err != nil {
		return failed(err)
	}
	if resetVars && c.SecureBoot != e.Config.SecureBoot && c.Firmware == core.FirmwareUEFI {
		if err := core.ResetVars(core.VMEntry{ID: e.ID, Path: e.Path, Config: c}); err != nil {
			return Result{Error: "saved, but the UEFI variables were not replaced: " + err.Error()}
		}
	}
	return Result{}
}

// NewVM is the New VM wizard's answers.
type NewVM struct {
	Name     string `json:"name"`
	Folder   string `json:"folder"` // "" = <Workspace>/<name>
	Guest    string `json:"guest"`
	Version  string `json:"version"` // macOS
	Profile  string `json:"profile"` // Linux, Windows, Other
	ISO      string `json:"iso"`
	Graphics string `json:"graphics"`
	RAMMiB   int    `json:"ramMiB"`
	VCPUs    int    `json:"vcpus"`
	// Firmware, Secure Boot and TPM of a Linux, Windows or Other VM.
	Firmware   string `json:"firmware"`
	SecureBoot bool   `json:"secureBoot"`
	TPM        bool   `json:"tpm"`
	// Disk is "new" (DiskGiB), "existing" (DiskPath) or "none" (a live ISO).
	Disk     string `json:"disk"`
	DiskGiB  int    `json:"diskGiB"`
	DiskPath string `json:"diskPath"`
	// Backend "reims-ctl": a macOS VM that reims-ctl creates (Offline: from its offline installer).
	Backend string `json:"backend"`
	Offline bool   `json:"offline"`
}

// cores splits vCPUs into cores of two threads when it can (as core.Recommended does).
func cores(vcpus int) int {
	if vcpus >= 2 && vcpus%2 == 0 {
		return vcpus / 2
	}
	return max(vcpus, 1)
}

// diskFormat reads an image's format from its header: qcow2 or raw.
func diskFormat(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err == nil && string(magic) == "QFI\xfb" {
		return "qcow2", nil
	}
	return "raw", nil
}

// guestConfig is the Config a New VM answer gives, validated as a whole before anything is created.
func guestConfig(n NewVM) (core.Config, error) {
	c, err := core.NewGuest(n.Profile, n.Name, n.ISO)
	if err != nil {
		return c, &core.FieldError{Field: "profile", Msg: err.Error()}
	}
	if c.Guest != n.Guest {
		return c, &core.FieldError{Field: "profile", Msg: "the profile is not a " + n.Guest + " profile"}
	}
	c.Workspace = n.Folder
	c.RAMMiB, c.VCPUs, c.Cores = n.RAMMiB, n.VCPUs, cores(n.VCPUs)
	c.Firmware, c.SecureBoot, c.TPM = n.Firmware, n.SecureBoot, n.TPM
	if n.Graphics != "" {
		c.Graphics = n.Graphics
	}
	bus := c.Disks[0].Bus
	switch n.Disk {
	case "none":
		c.Disks = nil
	case "existing":
		if !filepath.IsAbs(n.DiskPath) {
			return c, &core.FieldError{Field: "diskPath", Msg: "choose an existing disk image"}
		}
		format, err := diskFormat(n.DiskPath)
		if err != nil {
			return c, &core.FieldError{Field: "diskPath", Msg: err.Error()}
		}
		c.Disks = []core.Disk{{Path: n.DiskPath, Format: format, Bus: bus}}
	default:
		if n.DiskGiB < 1 || n.DiskGiB > 65536 {
			return c, &core.FieldError{Field: "diskGiB", Msg: "use a size between 1 and 65536 GiB"}
		}
		c.DiskGiB = n.DiskGiB
	}
	if n.ISO != "" && !filepath.IsAbs(n.ISO) {
		return c, &core.FieldError{Field: "iso", Msg: "use an absolute path to the ISO"}
	}
	if n.ISO != "" && !exists(n.ISO) {
		return c, &core.FieldError{Field: "iso", Msg: n.ISO + " does not exist"}
	}
	return c, nil
}

// macConfig is the macOS VM a New VM answer gives (CreateVMWithDefaults' choices plus the wizard's).
func macConfig(n NewVM) core.Config {
	c := core.Defaults()
	c.Name, c.Version, c.Workspace, c.Graphics = n.Name, n.Version, n.Folder, n.Graphics
	c.RAMMiB, c.VCPUs, c.Cores = n.RAMMiB, n.VCPUs, cores(n.VCPUs)
	c.Networks = core.NewNetworks(n.Version)
	return c
}

// CreateVM creates a VM from the wizard: core.CreateGuestVM for Linux, Windows and Other (it
// takes the wizard's memory, CPUs and disk choice, which core.CreateGuest does not), and
// core.CreateVMWithDefaults for macOS, then the wizard's memory, CPUs and graphics. Every answer is
// validated before anything is created; Field names the first invalid one.
func (a *App) CreateVM(n NewVM) Result {
	n.Name = strings.TrimSpace(n.Name)
	if n.Name == "" {
		return Result{Field: "name", Error: "give the VM a name"}
	}
	if vms, err := core.ListVMs(); err == nil && slices.ContainsFunc(vms, func(e core.VMEntry) bool { return strings.EqualFold(e.Config.Name, n.Name) }) {
		return Result{Field: "name", Error: "a VM with this name already exists"}
	}
	folder := n.Folder
	if folder == "" {
		folder = core.VMFolder(n.Name)
	}
	if n.Backend == core.BackendReimsCtl {
		return a.createReimsCtl(n)
	}
	if n.Guest == core.GuestMacOS {
		c := macConfig(n)
		check := c
		check.Workspace, check.SSHPort = folder, core.FirstSSHPort
		if err := check.Validate(); err != nil {
			return failed(err)
		}
		if err := checkHost(c); err != nil {
			return failed(err)
		}
		e, err := core.CreateVMWithDefaults(context.Background(), n.Name, n.Version, n.Folder, 0)
		if err != nil {
			return failed(err)
		}
		saved := e.Config
		saved.RAMMiB, saved.VCPUs, saved.Cores, saved.Graphics = c.RAMMiB, c.VCPUs, c.Cores, c.Graphics
		if err := core.SaveVM(e, saved); err != nil {
			return Result{ID: e.ID, Error: "created with the recommended hardware; the wizard's could not be saved: " + err.Error()}
		}
		return Result{ID: e.ID, Notice: e.Notice}
	}
	c, err := guestConfig(n)
	if err != nil {
		return failed(err)
	}
	check := c
	check.Workspace, check.SSHPort = folder, core.FirstSSHPort
	if err := check.Validate(); err != nil {
		return failed(err)
	}
	if err := checkHost(c); err != nil {
		return failed(err)
	}
	e, err := core.CreateGuestVM(context.Background(), c)
	if err != nil {
		return failed(err)
	}
	return Result{ID: e.ID, Notice: e.Notice}
}

// createReimsCtl is CreateVM through reims-ctl: its output goes to the page line by line as the
// "reims-create" event.
func (a *App) createReimsCtl(n NewVM) Result {
	if n.Guest != core.GuestMacOS {
		return Result{Field: "guest", Error: "reims-ctl creates macOS VMs only"}
	}
	if err := checkHost(core.Config{RAMMiB: n.RAMMiB}); err != nil {
		return failed(err)
	}
	if n.DiskGiB < 1 || n.DiskGiB > 65536 {
		return Result{Field: "diskGiB", Error: "use a size between 1 and 65536 GiB"}
	}
	o := core.ReimsCreate{Name: n.Name, Dir: n.Folder, MacOS: n.Version, Offline: n.Offline, CPUs: n.VCPUs,
		Memory: memArg(n.RAMMiB), DiskSize: fmt.Sprintf("%dG", n.DiskGiB)}
	e, err := core.CreateReimsCtlVM(context.Background(), o, func(line string) { a.emit("reims-create", line) })
	return created(e, err)
}

// created is the Result of a create or add: a VM registered before a later step failed keeps its id.
func created(e core.VMEntry, err error) Result {
	switch {
	case err != nil && e.ID == "":
		return failed(err)
	case err != nil:
		return Result{ID: e.ID, Error: err.Error()}
	}
	return Result{ID: e.ID, Notice: e.Notice}
}

// memArg is a memory size as reims-ctl takes it: 8G, or 1536M when it is not whole GiB.
func memArg(mib int) string {
	if mib%1024 == 0 {
		return fmt.Sprintf("%dG", mib/1024)
	}
	return fmt.Sprintf("%dM", mib)
}

// ReimsCtlCandidates lists the reims-ctl VMs that are not in the list yet, for Add.
func (a *App) ReimsCtlCandidates() ([]core.ReimsCtlCandidate, error) {
	return core.ReimsCtlCandidates(context.Background())
}

// AddReimsCtl adds reims-ctl's VM name from its configuration folder.
func (a *App) AddReimsCtl(name, configDir string) Result {
	return created(core.AddReimsCtlVM(context.Background(), name, configDir))
}

// AddVM adds an existing VM from its settings file (an export, or vm.json in its folder).
func (a *App) AddVM(path string) Result {
	return created(core.ImportSettings(path))
}

// DeleteVM removes a VM from the list; purge also deletes the files the app created for it.
func (a *App) DeleteVM(id string, purge bool) ([]string, error) {
	e, err := entry(id)
	if err != nil {
		return nil, err
	}
	if _, running := core.FindRun(e); running {
		return nil, errors.New("shut the VM down first")
	}
	return core.DeleteVM(e, purge)
}

// SelectVM remembers the selected VM, as the CLI's default.
func (a *App) SelectVM(id string) error { return core.SelectVM(id) }

// ShutDown presses the VM's power button through its control socket (as MCP's vm_shutdown).
func (a *App) ShutDown(id string) error {
	e, err := entry(id)
	if err != nil {
		return err
	}
	r, ok := core.FindRun(e)
	if !ok {
		return errors.New(e.DisplayName() + " is not running")
	}
	if r.OtherVM != "" {
		return errors.New(e.DisplayName() + "'s disk is open in VM " + r.OtherVM + "'s run: that run is not this VM's to control")
	}
	if r.Control == "" {
		return errors.New("this VM was started without a control socket: shut it down from inside the guest")
	}
	if err := core.CheckRunDir(r.Control); err != nil {
		return err
	}
	q, err := core.DialQMP(r.Control, nil, nil)
	if err != nil {
		return err
	}
	defer q.Close()
	return q.Execute("system_powerdown")
}

// Start runs the VM in its own session (core.StartDetached), so it keeps running when this window
// closes. Normal lets a Reims vGPU VM open its window on X11 for the app to take; Headless
// does not, and Reims vGPU, which always opens its own window, has no Headless.
func (a *App) Start(id string, headless bool) error {
	e, err := entry(id)
	if err != nil {
		return err
	}
	if _, running := core.FindRun(e); running {
		return errors.New(e.DisplayName() + " is already running")
	}
	if headless && e.Config.Graphics == core.GraphicsReims {
		return errors.New("Reims vGPU always opens its own window, so Headless is not available")
	}
	self, err := core.SelfPath()
	if err != nil {
		return err
	}
	// As MCP's vm_start: the downloads happen here, so the start's wait covers QEMU only.
	ctx := context.Background()
	ks, err := core.LoadExtraKexts()
	if err != nil {
		return err
	}
	if err := core.DownloadExtraKexts(ctx, ks); err != nil {
		return err
	}
	if e.Config.ReimsKext() {
		if err := core.DownloadReimsKext(ctx); err != nil {
			return err
		}
	}
	snapshot, err := json.Marshal(ks)
	if err != nil {
		return err
	}
	o := core.StartOptions{Starter: core.StarterGUI, EmbedX11: !headless && onX11(), KextsSnapshot: string(snapshot)}
	if headless {
		o.Display = core.DisplayApp // Normal keeps the VM's Display setting: QEMU's window when it says so
	}
	if _, err = core.StartDetached(ctx, self, e, o); err != nil {
		return err
	}
	return nil // the Manager shows the screen in its Screen tab; Show opens a window of its own
}

// Show opens the VM's window, or brings it to the front when it is open. Closing it leaves the VM running.
func (a *App) Show(id string) error {
	e, err := entry(id)
	if err != nil {
		return err
	}
	if a.show == nil {
		return errors.New("no window")
	}
	_, running := core.FindRun(e)
	a.show(e.ID, vmTitle(e.DisplayName(), running))
	return nil
}

// vmTitle is a VM window's title; the page keeps it current.
func vmTitle(name string, running bool) string {
	state := "Powered Off"
	if running {
		state = "Running"
	}
	return name + " [" + state + "] — kvm-gui"
}

// onX11 says whether the app runs in an X11 session, where it can hold Reims vGPU's window. Never
// on a Wayland desktop, even on XWayland: XWayland 24 crashes, taking every X11 client with it,
// when an app holding another client's window dies (scripts/check-gnome.sh 26.04, design D6).
func onX11() bool {
	if b := os.Getenv("GDK_BACKEND"); b != "" && !strings.HasPrefix(b, "x11") {
		return false
	}
	return os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") != ""
}

// logLimit is how much of a run log the Logs tool shows (its end).
const logLimit = 256 << 10

// Log is the newest run log of the VM (<state dir>/logs/<id>/*.log), its end when it is long.
// The GUI's detached starts write theirs there too (gui-start.log, core.StartDetached).
func (a *App) Log(id string) (string, error) {
	dir := filepath.Join(core.StateDir(), "logs", id)
	files, _ := filepath.Glob(filepath.Join(dir, "*.log"))
	newest, at := "", int64(0)
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && st.ModTime().UnixNano() > at {
			newest, at = f, st.ModTime().UnixNano()
		}
	}
	embedding := embedLines(id)
	if newest == "" {
		return embedding, nil
	}
	f, err := os.Open(newest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > logLimit {
		f.Seek(-logLimit, io.SeekEnd)
	}
	b, err := io.ReadAll(f)
	return newest + "\n\n" + string(b) + embedding, err
}

// embedLines is what embedding did with VM id (embed.log), the last 50 lines, for its Logs tool.
func embedLines(id string) string {
	b, err := os.ReadFile(filepath.Join(core.StateDir(), "logs", "embed.log"))
	if err != nil {
		return ""
	}
	var mine []string
	for l := range strings.Lines(string(b)) {
		if strings.Contains(l, " "+id+": ") {
			mine = append(mine, l)
		}
	}
	if len(mine) == 0 {
		return ""
	}
	return "\n--- Reims vGPU's window in the app (" + filepath.Join(core.StateDir(), "logs", "embed.log") + ")\n" + strings.Join(mine[max(0, len(mine)-50):], "")
}

// Medium is a disk or optical image the VMs use (File › Media manager).
type Medium struct {
	DiskInfo
	Kind string   `json:"kind"` // "disk" or "optical"
	VMs  []string `json:"vms"`
}

// Media lists the hard disk and optical images the VMs know, each once, with the VMs using it.
func (a *App) Media() ([]Medium, error) {
	entries, err := core.ListVMs()
	if err != nil {
		return nil, err
	}
	var out []Medium
	add := func(kind string, d DiskInfo, vm string) {
		for i := range out {
			if out[i].Path == d.Path {
				if !slices.Contains(out[i].VMs, vm) {
					out[i].VMs = append(out[i].VMs, vm)
				}
				return
			}
		}
		out = append(out, Medium{DiskInfo: d, Kind: kind, VMs: []string{vm}})
	}
	for _, e := range entries {
		name := e.DisplayName()
		for _, d := range disks(e.Config) {
			add("disk", d, name)
		}
		if dir, err := e.Config.WorkspaceDir(); err == nil && e.Config.MacOS() {
			if p := filepath.Join(dir, core.MediaFile); exists(p) {
				add("optical", diskInfo(p, "raw", "sata"), name)
			}
		}
		for _, iso := range e.Config.Optical {
			if iso != "" {
				add("optical", diskInfo(iso, "raw", "sata"), name)
			}
		}
	}
	return out, nil
}
