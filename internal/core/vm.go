package core

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// osk is the isa-applesmc key, the same as in OpenCore-Boot.sh.
const osk = "ourhardworkbythesewordsguardedpleasedontsteal(c)AppleComputerInc"

// qemuPath escapes commas, which separate QEMU options.
func qemuPath(p string) string { return strings.ReplaceAll(p, ",", ",,") }

// BuildArgs is OpenCore-Boot.sh ported: config → qemu-system-x86_64 arguments.
func BuildArgs(c Config, w Workspace, qmpSocket string, withMedia bool, gpuFuncs []string) []string {
	p := ProfileFor(c.Version)
	f := func(name string) string { return qemuPath(w.Path(name)) }
	args := []string{
		"-name", "macOS",
		"-enable-kvm", "-m", strconv.Itoa(c.RAMMiB), "-cpu", p.CPU,
		"-machine", "q35",
		"-device", "qemu-xhci,id=xhci",
		"-device", "usb-kbd,bus=xhci.0", "-device", "usb-tablet,bus=xhci.0",
		"-smp", fmt.Sprintf("%d,sockets=1,cores=%d,threads=%d", c.VCPUs, c.Cores, c.VCPUs/c.Cores),
		"-device", "usb-ehci,id=ehci",
		"-device", "isa-applesmc,osk=" + osk,
		"-drive", "if=pflash,format=raw,readonly=on,file=" + f(OVMFCode),
		"-drive", "if=pflash,format=raw,file=" + f(VarsFile),
		"-smbios", "type=2",
		"-device", "ich9-intel-hda", "-device", "hda-duplex",
		"-device", "ich9-ahci,id=sata",
		// OpenCore boots from the extracted EFI directory, exposed read-only as a FAT disk by
		// QEMU's vvfat driver. SATA refuses read-only drives, so it sits on virtio-blk.
		"-drive", "id=OpenCoreBoot,if=none,format=raw,readonly=on,file=fat:" + f(EFIDir),
		"-device", "virtio-blk-pci,drive=OpenCoreBoot,bootindex=0",
	}
	if c.Reims() {
		args = reimsArgs(args, c.RAMMiB)
	}
	if withMedia {
		args = append(args,
			"-device", "ide-hd,bus=sata.3,drive=InstallMedia",
			"-drive", "id=InstallMedia,if=none,format=raw,file="+f(MediaFile))
		// The cached full installer as a disc: the recovery installs from it with no network.
		// SATA refuses read-only drives; snapshot=on keeps the guest's writes off the shared file.
		if ci, ok := CachedInstallerFor(c.Version); ok {
			args = append(args,
				"-device", "ide-hd,bus=sata.2,drive=InstallerDisc",
				"-drive", "id=InstallerDisc,if=none,format=raw,snapshot=on,file="+qemuPath(ci.Path))
		}
	}
	args = append(args,
		"-drive", "id=MacHDD,if=none,format=qcow2,file="+f(DiskFile),
		"-device", "ide-hd,bus=sata.4,drive=MacHDD",
	)
	firstNIC, moreNICs := netArgs(c, BridgeHelper())
	args = append(args, firstNIC...)
	// vmware-svga stays even with a passed-through GPU: QEMU's window needs a graphical
	// console to deliver keyboard and mouse input, and it shows the boot screens.
	if c.Reims() {
		// No addr: upstream's 0x4 is taken by ich9-ahci, and OpenCore boots by scanning.
		args = append(args, "-device", "apple-gfx-pci,id=gfx,displays=1,resolution="+c.ReimsResolution())
	} else {
		args = append(args, "-device", "vmware-svga")
	}
	if len(gpuFuncs) > 0 {
		args = append(args, "-device", "vfio-pci,host="+gpuFuncs[0]+",multifunction=on,x-no-kvm-intx=on")
		for _, f := range gpuFuncs[1:] {
			args = append(args, "-device", "vfio-pci,host="+f)
		}
	}
	args = append(args, moreNICs...)
	return append(args, runArgs(c, qmpSocket)...)
}

// runArgs are the run's display and control sockets, the same for every guest type.
func runArgs(c Config, qmpSocket string) []string {
	var args []string
	if c.InApp() || c.Reims() {
		// A Reims VM's screen is always the library's own window (-display none); the VNC
		// socket still serves the Preview and screen capture.
		// No QEMU window: the screen goes to the app over VNC on a Unix socket in the same
		// private 0700 directory as QMP, never on a TCP port.
		// No -k: with it, QEMU remaps extended key events through that layout's keysyms and drops
		// the physical scancode, so keys a US layout lacks (ABNT2's ç and "/?", the ISO key left
		// of Z) were lost. Without it the scancode goes through and macOS's input source applies.
		// lock-key-sync=off keeps QEMU from injecting Caps Lock presses inferred from keysyms.
		args = append(args, "-display", "none", "-vnc", "unix:"+qemuPath(VNCSocket(qmpSocket))+",lock-key-sync=off")
	}
	// A second monitor for one other process (the MCP server or an attached app): QMP serves
	// one client per monitor, and the starter keeps the first.
	return append(args, "-qmp", "unix:"+qemuPath(qmpSocket)+",server=on,wait=off",
		"-qmp", "unix:"+qemuPath(ControlSocket(qmpSocket))+",server=on,wait=off")
}

// ControlSocket is the second QMP socket next to a run's QMP socket.
func ControlSocket(qmpSocket string) string {
	return filepath.Join(filepath.Dir(qmpSocket), "qmp-control.sock")
}

// VNCSocket is the VNC socket next to a VM run's QMP socket.
func VNCSocket(qmpSocket string) string {
	return filepath.Join(filepath.Dir(qmpSocket), "vnc.sock")
}

// memlockLimit is the soft RLIMIT_MEMLOCK in bytes (VFIO pins all guest RAM).
func memlockLimit() uint64 {
	var r syscall.Rlimit
	if err := syscall.Getrlimit(8 /* RLIMIT_MEMLOCK on Linux */, &r); err != nil {
		return 0
	}
	return r.Cur
}

// explainExit turns QEMU output into a message for the user ("" = normal exit).
func explainExit(err error, lastLines []string) string {
	out := strings.Join(lastLines, "\n")
	switch {
	case strings.Contains(out, "failed to initialize kvm: Device or resource busy"):
		return "KVM is busy: another hypervisor (VirtualBox or VMware) is using virtualization. Shut its VM down and try again."
	case strings.Contains(out, `Failed to get "write" lock`):
		return "The disk is already open in another QEMU (started outside the app?). Shut that VM down first."
	case strings.Contains(out, "Could not set up host forwarding rule"):
		return "The SSH port is already in use on 127.0.0.1 (another VM?). Choose another port in Settings › Network."
	case strings.Contains(out, "bridge helper") || strings.Contains(out, "access denied by acl file") ||
		strings.Contains(out, "failed to create tun device") || strings.Contains(out, "qemu-bridge-helper"):
		return "A bridged adapter could not attach: qemu-bridge-helper needs to be setuid root (or hold CAP_NET_ADMIN) and the bridge must be allowed in /etc/qemu/bridge.conf. See Troubleshooting › Bridged networking."
	case err == nil:
		return ""
	case udmabufExit(out) != "":
		return udmabufExit(out)
	case strings.Contains(out, "Could not access KVM kernel module"), strings.Contains(out, "/dev/kvm"):
		return "No access to KVM. Check the Host tab."
	}
	last := ""
	for i := len(lastLines) - 1; i >= 0; i-- { // warnings such as the "pcid" one do not explain the exit
		if !strings.Contains(lastLines[i], "warning:") {
			last = ": " + lastLines[i]
			break
		}
	}
	return "QEMU exited with an error (" + err.Error() + ")" + last
}

type VMState int

const (
	Stopped VMState = iota
	Running
	Paused
)

func (s VMState) String() string {
	return [...]string{"Stopped", "Running", "Paused (error)"}[s]
}

// VM controls a QEMU started by this app instance.
type VM struct {
	OnState func(VMState, string) // state and message (error or warning)
	OnLog   func(string)
	// EmbedX11 (set by the GUI before Start, never by the CLI or MCP): a Reims vGPU VM opens
	// its window on X11 (SDL_VIDEO_DRIVER=x11) so the app can embed it in the Screen tab.
	EmbedX11 bool
	// ID and Starter (the VM's id, and StarterGUI, StarterCLI or StarterMCP): the run uses the
	// VM's run directory and writes run.json there, so every other process finds it as this VM's
	// run (design D4). Without an ID the run gets a temporary directory and no record.
	ID, Starter string

	mu      sync.Mutex
	proc    *os.Process
	qmp     *QMP
	vncSock string // set while running with the in-app display or Reims vGPU
	disc    bool   // the run has the offline installer disc attached
	state   VMState
	ioError string
	killed  bool
}

func (v *VM) State() VMState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.state
}

func (v *VM) setState(s VMState, msg string) {
	v.mu.Lock()
	v.state = s
	v.mu.Unlock()
	if v.OnState != nil {
		v.OnState(s, msg)
	}
}

func (v *VM) log(s string) {
	if v.OnLog != nil {
		v.OnLog(s)
	}
}

// Start validates everything, takes the workspace lock (held until QEMU exits) and starts QEMU.
func (v *VM) Start(c Config) error {
	if v.State() != Stopped {
		return errors.New("the VM is already running")
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if !c.MacOS() {
		return v.startGuest(c)
	}
	dir, _ := c.WorkspaceDir()
	w := NewWorkspace(dir)
	if !w.Exists() {
		return fmt.Errorf("VM folder %s does not exist: create it in the Install tab", dir)
	}
	if !w.HasDisk() {
		return fmt.Errorf("%s does not exist: create the disk in the Install tab", DiskFile)
	}
	if r, ok := v.findRun(c); ok {
		who := "this app"
		if !r.ByApp {
			who = "another program (for example OSX-KVM's OpenCore-Boot.sh)"
		}
		return fmt.Errorf("this VM's disk is already in use by QEMU (pid %d), started by %s: shut that VM down first", r.PID, who)
	}
	if err := checkBridges(c, "/sys/class/net", "/etc/qemu/bridge.conf", BridgeHelper()); err != nil {
		return err
	}
	if err := checkSSHPort(c); err != nil {
		return err
	}
	if c.Extras == nil { // callers that downloaded the kexts pass the snapshot they used
		ks, err := LoadExtraKexts()
		if err != nil {
			return err
		}
		c.Extras = &ks
	}
	var gpuFuncs []string
	if c.GPU != "" {
		var err error
		if gpuFuncs, err = SystemHost().GPUReady(c, memlockLimit()); err != nil {
			return err
		}
	}
	qemu := SystemQEMU()
	if c.Reims() {
		bin, warnings, err := SystemHost().ReimsReady(c)
		if err != nil {
			return err
		}
		for _, msg := range warnings {
			v.log("warning: Reims vGPU: " + msg)
		}
		qemu = bin
	}
	unlock, err := v.lockRun(w, c)
	if err != nil {
		return err
	}
	fail := func(err error) error { unlock(); return err }
	if err := w.PrepareBoot(c, w.HasMedia()); err != nil {
		return fail(fmt.Errorf("could not prepare the boot files: %w", err))
	}
	qmpDir, err := v.newRunDir()
	if err != nil {
		return fail(err)
	}
	sock := filepath.Join(qmpDir, "qmp.sock")
	// The installer disc BuildArgs attaches is held (a shared lock) for the whole run, so no
	// process deletes it meanwhile.
	var discDone func()
	if ci, ok := CachedInstallerFor(c.Version); ok && w.HasMedia() {
		if err := RefreshInstallerDisc(ci); err != nil {
			v.log("[installer] " + err.Error())
		}
		if done, err := useDisc(ci.Path); err == nil {
			discDone = done
		}
	}
	args := BuildArgs(c, w, sock, w.HasMedia(), gpuFuncs)
	if err := checkDevices(qemu, args); err != nil {
		if discDone != nil {
			discDone()
		}
		os.RemoveAll(qmpDir)
		return fail(err)
	}
	v.log(qemu + " " + strings.Join(args, " "))

	var env []string
	if c.Reims() {
		env = reimsEnv(os.Environ(), v.EmbedX11)
	}
	return v.launch(launchSpec{
		qemu: qemu, args: args, dir: dir, env: env, sock: sock, qmpDir: qmpDir, vnc: c.InApp() || c.Reims(), disc: discDone != nil,
		afterExit: func(ran time.Duration) {
			if discDone != nil {
				discDone()
			}
			w.ClearRootPatchMarker(c, ran)
		},
		unlock: unlock,
	})
}

// findRun is the run that keeps c from starting: through its disks, and through its run record.
func (v *VM) findRun(c Config) (RunInfo, bool) {
	if v.ID == "" {
		return FindRunning(c)
	}
	return FindRun(VMEntry{ID: v.ID, Config: c})
}

// newRunDir is the run's private directory (0700), created under the VM's lock: the VM's own run
// directory when ID is set, else a temporary one.
func (v *VM) newRunDir() (string, error) {
	if v.ID != "" {
		return makeRunDir(v.ID)
	}
	return os.MkdirTemp(runBase(), "macos-kvm-gui-")
}

// launchSpec is one QEMU run: its command, its private run directory, and what to undo after it.
type launchSpec struct {
	qemu      string
	args, env []string
	dir       string // QEMU's working directory (the VM folder)
	sock      string // the starter's QMP socket
	qmpDir    string // the run's private directory, removed after the run
	vnc       bool   // the run serves the VNC socket next to sock
	disc      bool   // the offline installer disc is attached
	afterExit func(ran time.Duration)
	unlock    func() // the VM's locks, released last
}

// launch starts QEMU and returns once it runs; a goroutine waits for it, cleans up and reports
// Stopped. On a start failure everything is undone before launch returns.
func (v *VM) launch(l launchSpec) error {
	started := make(chan error, 1)
	go func() {
		// Pdeathsig fires when the thread that created the process dies: it stays
		// locked to this goroutine, which only ends after Wait.
		runtime.LockOSThread()
		cmd := exec.Command(l.qemu, l.args...)
		cmd.Dir = l.dir
		if l.env != nil {
			cmd.Env = l.env
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM, Setpgid: true} // Ctrl+C in a terminal must not reach QEMU
		pr, pw := io.Pipe()
		cmd.Stdout, cmd.Stderr = pw, pw
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		if v.ID != "" {
			if err := writeRunRecord(l.qmpDir, v.ID, v.Starter, cmd.Process.Pid); err != nil {
				v.log("warning: could not write run.json: " + err.Error())
			}
		}
		v.mu.Lock()
		v.proc, v.ioError, v.vncSock, v.disc = cmd.Process, "", "", l.disc
		if l.vnc {
			v.vncSock = VNCSocket(l.sock)
		}
		v.mu.Unlock()
		v.setState(Running, "") // before any Stopped from this same goroutine
		started <- nil

		var tail []string
		logDone := make(chan struct{})
		go func() {
			defer close(logDone)
			sc := bufio.NewScanner(pr)
			for sc.Scan() {
				line := sc.Text()
				tail = append(tail, line)
				if len(tail) > 20 {
					tail = tail[1:]
				}
				v.log(line)
			}
		}()
		go v.connectQMP(l.sock)

		startedAt := time.Now()
		err := cmd.Wait()
		pw.Close()
		<-logDone
		v.mu.Lock()
		q, killed := v.qmp, v.killed
		v.proc, v.qmp, v.killed, v.vncSock, v.disc = nil, nil, false, "", false
		v.mu.Unlock()
		if q != nil {
			q.Close()
		}
		if l.afterExit != nil {
			l.afterExit(time.Since(startedAt))
		}
		os.RemoveAll(l.qmpDir)
		l.unlock()
		msg := explainExit(err, tail)
		if killed {
			msg = "Stopped by the user (forced)."
		}
		v.setState(Stopped, msg)
	}()
	if err := <-started; err != nil {
		if l.afterExit != nil {
			l.afterExit(0)
		}
		os.RemoveAll(l.qmpDir)
		l.unlock()
		return fmt.Errorf("could not start QEMU: %w", err)
	}
	return nil
}

func (v *VM) connectQMP(sock string) {
	q, err := DialQMP(sock, v.onEvent, func() {
		v.mu.Lock()
		quiet := v.killed || v.proc == nil
		v.mu.Unlock()
		if !quiet {
			v.log("warning: QMP connection closed; graceful shutdown is unavailable")
		}
	})
	if err != nil {
		v.log("warning: " + err.Error())
		return
	}
	v.mu.Lock()
	if v.proc == nil { // QEMU already exited
		v.mu.Unlock()
		q.Close()
		return
	}
	v.qmp = q
	v.mu.Unlock()
}

func (v *VM) onEvent(e QMPEvent) {
	switch e.Name {
	case "BLOCK_IO_ERROR":
		if e.Data["action"] != "stop" {
			return
		}
		msg := fmt.Sprintf("I/O error on disk %v (%v)", e.Data["device"], e.Data["operation"])
		if e.Data["nospace"] == true {
			msg += ": out of space on the host"
		}
		v.mu.Lock()
		v.ioError = msg
		v.mu.Unlock()
		v.setState(Paused, msg)
	case "STOP":
		v.mu.Lock()
		msg := v.ioError
		v.mu.Unlock()
		if msg == "" {
			msg = "QEMU paused the guest"
		}
		v.setState(Paused, msg)
	case "RESUME":
		v.setState(Running, "")
	}
}

// Powerdown asks the guest to shut down; the state only changes when QEMU exits.
// VNCSocket returns the VNC socket of the running VM, or "" when it is stopped or uses
// QEMU's own window.
// Pid is QEMU's process id while it runs, else 0.
func (v *VM) Pid() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.proc == nil {
		return 0
	}
	return v.proc.Pid
}

func (v *VM) VNCSocket() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.vncSock
}

// InstallerCommand is the line to type in the recovery's Terminal to install from the attached
// installer disc, or "" when the VM has none.
func (v *VM) InstallerCommand() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.disc {
		return ""
	}
	return DiscCommand
}

func (v *VM) Powerdown() error {
	v.mu.Lock()
	q := v.qmp
	v.mu.Unlock()
	if q == nil {
		return errors.New("QMP control unavailable: use Force stop")
	}
	return q.Execute("system_powerdown")
}

// Kill ends QEMU immediately (like pulling the plug).
func (v *VM) Kill() error {
	v.mu.Lock()
	p := v.proc
	if p != nil {
		v.killed = true
	}
	v.mu.Unlock()
	if p == nil {
		return errors.New("the VM is not running")
	}
	return p.Kill()
}
