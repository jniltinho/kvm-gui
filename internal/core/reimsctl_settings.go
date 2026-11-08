package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ErrReimsCtlUnsupported is what every refused action on a reims-ctl VM wraps, so the GUI, the
// CLI and MCP refuse it the same way (design D19).
var ErrReimsCtlUnsupported = errors.New("not available for a reims-ctl VM yet")

// refuseReimsCtl refuses action on a reims-ctl VM.
func refuseReimsCtl(c Config, action string) error {
	if c.Backend == BackendReimsCtl {
		return fmt.Errorf("%s: %w", action, ErrReimsCtlUnsupported)
	}
	return nil
}

// ReimsSettings are the settings shown for a reims-ctl VM, read from the command reims-ctl
// prints, so a change made with reims-ctl itself is seen.
type ReimsSettings struct {
	CPUs       int
	MemoryMiB  int
	Resolution string
	Displays   int
	SSHPort    int
	MAC        string
}

// settings reads them from the command's options.
func (c ReimsCommand) settings() ReimsSettings {
	var s ReimsSettings
	for _, v := range c.values("-smp") {
		s.CPUs, _ = strconv.Atoi(strings.Split(v, ",")[0])
	}
	for _, v := range c.values("-m") {
		s.MemoryMiB = memMiB(v)
	}
	for _, d := range c.values("-device") {
		if strings.HasPrefix(d, "apple-gfx-pci,") {
			s.Resolution = suboption(d, "resolution")
			s.Displays, _ = strconv.Atoi(suboption(d, "displays"))
		}
		if m := suboption(d, "mac"); m != "" && s.MAC == "" {
			s.MAC = m
		}
	}
	for _, n := range c.values("-netdev") {
		if _, fwd, ok := strings.Cut(n, "hostfwd=tcp:127.0.0.1:"); ok && strings.Contains(fwd, "-:22") {
			p, _, _ := strings.Cut(fwd, "-")
			s.SSHPort, _ = strconv.Atoi(p)
		}
	}
	return s
}

// memMiB reads QEMU's -m size (8192M, 8G, or MiB without a suffix).
func memMiB(v string) int {
	v = strings.Split(v, ",")[0]
	mult := 1
	switch {
	case strings.HasSuffix(v, "G"):
		mult, v = 1024, strings.TrimSuffix(v, "G")
	case strings.HasSuffix(v, "M"):
		v = strings.TrimSuffix(v, "M")
	}
	n, _ := strconv.Atoi(v)
	return n * mult
}

// apply copies them into c, the form the rest of the app shows and checks.
func (s ReimsSettings) apply(c *Config) {
	c.Graphics, c.Display = GraphicsReims, DisplayApp
	if s.CPUs > 0 {
		c.VCPUs, c.Cores = s.CPUs, s.CPUs
	}
	if s.MemoryMiB > 0 {
		c.RAMMiB = s.MemoryMiB
	}
	c.Resolution, c.SSHPort = s.Resolution, s.SSHPort
}

// RefreshReimsCtlVM re-reads a reims-ctl VM's folder, disks and settings from reims-ctl and
// stores what changed, so the list, the port inventory and its warnings see changes made with
// reims-ctl itself.
func RefreshReimsCtlVM(ctx context.Context, e VMEntry) (VMEntry, error) {
	if e.Config.Backend != BackendReimsCtl {
		return e, nil
	}
	r, err := reimsCtlFor(e.Config)
	if err != nil {
		return e, err
	}
	c := e.Config
	if err := refreshReimsCtl(ctx, r, &c); err != nil {
		return e, err
	}
	db, err := openDB()
	if err != nil {
		return e, err
	}
	defer db.Close()
	if err := updateVM(db, e.ID, c); err != nil {
		return e, err
	}
	e.Config = c
	return e, nil
}

// refreshAllReimsCtl refreshes every reims-ctl VM before ports are counted; one that reims-ctl
// cannot read keeps its stored settings.
func refreshAllReimsCtl(ctx context.Context) {
	vms, err := ListVMs()
	if err != nil {
		return
	}
	for _, e := range vms {
		if e.Config.Backend == BackendReimsCtl {
			RefreshReimsCtlVM(ctx, e)
		}
	}
}

// ReimsSet is a change of a reims-ctl VM's settings; zero values (nil for yes/no) stay as they are.
type ReimsSet struct {
	CPUs, MemoryMiB, Displays, SSHPort, StopTimeout int
	Resolution, GPU, MAC                            string
	HiDPI, Audio                                    *bool
}

// args are the `reims-ctl set` options for the change.
func (s ReimsSet) args() []string {
	var a []string
	add := func(k, v string) { a = append(a, k, v) }
	if s.CPUs > 0 {
		add("--cpus", strconv.Itoa(s.CPUs))
	}
	if s.MemoryMiB > 0 {
		if s.MemoryMiB%1024 == 0 {
			add("--memory", strconv.Itoa(s.MemoryMiB/1024)+"G")
		} else {
			add("--memory", strconv.Itoa(s.MemoryMiB)+"M")
		}
	}
	if s.Displays > 0 {
		add("--displays", strconv.Itoa(s.Displays))
	}
	if s.Resolution != "" {
		add("--resolution", s.Resolution)
	}
	if s.HiDPI != nil {
		add("--hidpi", strconv.FormatBool(*s.HiDPI))
	}
	if s.GPU != "" {
		add("--gpu", s.GPU)
	}
	if s.Audio != nil {
		add("--audio", strconv.FormatBool(*s.Audio))
	}
	if s.SSHPort > 0 {
		add("--ssh-port", strconv.Itoa(s.SSHPort))
	}
	if s.MAC != "" {
		add("--mac", s.MAC)
	}
	if s.StopTimeout > 0 {
		add("--stop-timeout", strconv.Itoa(s.StopTimeout))
	}
	return a
}

// SetReimsCtl writes a change with `reims-ctl set` (the only way the app changes these VMs),
// then reads the VM back.
func SetReimsCtl(ctx context.Context, e VMEntry, s ReimsSet) (VMEntry, error) {
	if e.Config.Backend != BackendReimsCtl {
		return e, errors.New(e.DisplayName() + " is not a reims-ctl VM")
	}
	if _, ok := FindRun(e); ok {
		return e, errors.New("the VM is running: shut the VM down first")
	}
	args := s.args()
	if len(args) == 0 {
		return e, nil
	}
	r, err := reimsCtlFor(e.Config)
	if err != nil {
		return e, err
	}
	if _, err := r.run(ctx, nil, append([]string{"set", e.Config.ReimsCtlName}, args...)...); err != nil {
		return e, err
	}
	return RefreshReimsCtlVM(ctx, e)
}

// saveReimsCtl is SaveVM for a reims-ctl VM: the name is the app's, the rest goes to reims-ctl.
func saveReimsCtl(e VMEntry, c Config) error {
	old := e.Config
	if c.GPU != "" {
		return refuseReimsCtl(c, "GPU passthrough")
	}
	if c.Workspace != old.Workspace {
		return refuseReimsCtl(c, "moving its folder")
	}
	if c.Graphics != old.Graphics || c.Display != old.Display || c.Version != old.Version || c.RootPatch != old.RootPatch {
		return refuseReimsCtl(c, "changing the graphics, screen, version or root-patch mode")
	}
	var s ReimsSet
	if c.VCPUs != old.VCPUs {
		s.CPUs = c.VCPUs
	}
	if c.RAMMiB != old.RAMMiB {
		s.MemoryMiB = c.RAMMiB
	}
	if c.Resolution != old.Resolution {
		s.Resolution = c.Resolution
	}
	if c.SSHPort != old.SSHPort {
		s.SSHPort = c.SSHPort
	}
	ctx := context.Background()
	e2, err := SetReimsCtl(ctx, e, s)
	if err != nil {
		return err
	}
	e2.Config.Name, e2.Config.Description = c.Name, c.Description
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	return updateVM(db, e.ID, e2.Config)
}

// UserReimsCtlConfig is the user's own reims-ctl configuration folder ($XDG_CONFIG_HOME/reims-ctl).
func UserReimsCtlConfig() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(base) {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "reims-ctl")
}

// ReimsCtlCandidate is a reims-ctl VM the app has not registered yet, in ConfigDir.
type ReimsCtlCandidate struct {
	ReimsCtlVM
	ConfigDir string
}

// ReimsCtlCandidates lists, for the Manager's Add, the VMs of kvm-gui's reims-ctl folders and of
// the user's own reims-ctl that are not registered yet. A folder reims-ctl cannot read is skipped.
func ReimsCtlCandidates(ctx context.Context) ([]ReimsCtlCandidate, error) {
	base, err := reimsCtlFor(Config{})
	if err != nil {
		return nil, err
	}
	registered, err := ListVMs()
	if err != nil {
		return nil, err
	}
	var out []ReimsCtlCandidate
	for _, dir := range []string{base.ConfigDir, UserReimsCtlConfig()} {
		r, _ := reimsCtlFor(Config{ReimsCtlConfig: dir})
		vms, err := r.List(ctx)
		if err != nil {
			continue
		}
		for _, v := range vms {
			if !slices.ContainsFunc(registered, func(e VMEntry) bool {
				return e.Config.Backend == BackendReimsCtl && e.Config.ReimsCtlName == v.Name && e.Config.ReimsCtlConfig == dir
			}) {
				out = append(out, ReimsCtlCandidate{v, dir})
			}
		}
	}
	return out, nil
}

// AddReimsCtlVM registers reims-ctl's VM name from configDir ("" = kvm-gui's), recording that
// folder for every later reims-ctl call. When its SSH port belongs to another VM, reims-ctl gets
// the free port CreateVM picked (the Notice says so).
func AddReimsCtlVM(ctx context.Context, name, configDir string) (VMEntry, error) {
	base, err := reimsCtlFor(Config{})
	if err != nil {
		return VMEntry{}, err
	}
	if configDir == "" {
		configDir = base.ConfigDir
	}
	c := Config{Name: name, Backend: BackendReimsCtl, ReimsCtlName: name, ReimsCtlConfig: configDir}
	if err := c.validateReimsCtl(); err != nil {
		return VMEntry{}, err
	}
	r, err := reimsCtlFor(c)
	if err != nil {
		return VMEntry{}, err
	}
	if err := refreshReimsCtl(ctx, r, &c); err != nil {
		return VMEntry{}, err
	}
	port := c.SSHPort
	e, err := CreateVM(c)
	if err != nil {
		return VMEntry{}, err
	}
	if port != 0 && e.Config.SSHPort != port {
		if _, err := r.run(ctx, nil, "set", name, "--ssh-port", strconv.Itoa(e.Config.SSHPort)); err != nil {
			return e, err
		}
	}
	return e, nil
}
