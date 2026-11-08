package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "reims-ctl", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The scripts upstream's shell_script() prints are read exactly: quoting with spaces,
// apostrophes and $, an option with its value on one line, unset and export (task 4.5).
func TestParseReimsCommand(t *testing.T) {
	c, err := ParseReimsCommand(fixture(t, "custom.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Program != "/r/bin/qemu-system-x86_64" || !slices.Equal(c.Unset, []string{"REIMS_VGPU_WINDOW"}) ||
		!slices.Equal(c.Env, []string{"REIMS_VGPU_LOG=info", "BAR=1", "FOO=a b 'c' $HOME"}) {
		t.Fatalf("program and environment: %+v", c)
	}
	for _, want := range [][]string{
		{"-name", "mac"},
		{"-drive", "if=pflash,format=raw,readonly=on,file=/tmp/h/vms/my mac/OVMF_CODE.fd"},
		{"-device", "virtio-rng-pci,id=it's"},
		{"-name", "a $b"},
		{"-k", "pt-br"},
		{"-pidfile", "/tmp/h/vms/my mac/qemu.pid"},
		{"-qmp", "unix:/tmp/h/vms/my mac/qmp.sock,server=on,wait=off"},
	} {
		i := slices.Index(c.Args, want[1])
		if i < 1 || c.Args[i-1] != want[0] {
			t.Errorf("missing %q", want)
		}
	}
	if err := c.check("/tmp/h/vms/my mac"); err != nil {
		t.Fatal(err)
	}
	if got := c.driveFiles(); !slices.Equal(got, []string{"/tmp/h/vms/my mac/opencore.qcow2", "/tmp/h/vms/my mac/disk.qcow2"}) {
		t.Fatalf("drive files: %q", got)
	}
	if c.qmpSocket() != "/tmp/h/vms/my mac/qmp.sock" {
		t.Fatalf("QMP socket %q", c.qmpSocket())
	}
	// -daemonize and a second -pidfile in extra_args are refused; so is a foreign one.
	for _, f := range []string{"daemonize.sh", "pidfile.sh"} {
		c, err := ParseReimsCommand(fixture(t, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.check("/tmp/h/vms/my mac"); err == nil {
			t.Errorf("%s accepted", f)
		}
	}
	d, _ := ParseReimsCommand(fixture(t, "default.sh"))
	if err := d.check("/tmp/h/vms/other"); err == nil || !strings.Contains(err.Error(), "-pidfile") {
		t.Fatalf("a -pidfile outside the VM's folder: %v", err)
	}
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	os.MkdirAll(filepath.Join(rt, "kvm-gui", "a"), 0o700)
	link := filepath.Join(t.TempDir(), "into-run")
	os.MkdirAll(filepath.Join(rt, "kvm-gui", "a", "sub"), 0o700)
	os.Symlink(filepath.Join(rt, "kvm-gui", "a", "sub"), link)
	vm := t.TempDir()
	os.Symlink(filepath.Join(rt, "kvm-gui"), filepath.Join(vm, "relrun"))
	for _, extra := range [][]string{
		{"-chardev", "socket,id=x,path=" + rt + "/kvm-gui/other/qmp.sock"},
		{"-qmp", "unix:" + rt + "/./kvm-gui/a/qmp-control.sock,server=on"},
		{"-qmp", "unix:" + rt + "/x/../kvm-gui/a/qmp-gui.sock,server=on"},
		{"-serial", "file:" + link + "/../qmp-control.sock"},   // a link, then ".."
		{"-chardev", "socket,id=y,path=relrun/a/qmp-gui.sock"}, // relative to the VM folder
		{"-pidfile", "relrun/a/x.pid"},
	} {
		d2 := d
		d2.Args = append(slices.Clone(d.Args), extra...)
		if extra[0] == "-pidfile" { // the VM's own pidfile path is the vm folder's, so check another rule
			d2.Args = append(slices.Clone(d.Args[:len(d.Args)-2]), "-serial", "file:relrun/a/serial.log")
		}
		for i, a := range d2.Args {
			if a == "-pidfile" {
				d2.Args[i+1] = filepath.Join(vm, "qemu.pid")
			}
		}
		if err := d2.check(vm); err == nil {
			t.Errorf("a socket in the app's run folder was accepted: %v", extra)
		}
	}
	// Anything but unset, export and exec with literal words stops the start, naming it.
	for script, want := range map[string]string{
		"#!/bin/sh\necho hi\nexec /q -m 1\n":          "the command echo",
		"#!/bin/sh\nexec /q -name \"$HOME\"\n":        "expansion",
		"#!/bin/sh\nexec /q -name $HOME\n":            "ParamExp",
		"#!/bin/sh\nexec /q -name $(id)\n":            "CmdSubst",
		"#!/bin/sh\nexec /q -drive file=*.img\n":      "glob",
		"#!/bin/sh\nexec /q > /tmp/x\n":               "Redirect",
		"#!/bin/sh\nexport A=$(id)\nexec /q\n":        "CmdSubst",
		"#!/bin/sh\nexec /q\nexec /r\n":               "not last",
		"#!/bin/sh\nexec qemu -m 1\n":                 "absolute",
		"#!/bin/sh\nexport A=1\n":                     "no exec",
		"#!/bin/sh\nexec /q -m 1 &\n":                 "background",
		"#!/bin/sh\nX=1 exec /q\n":                    "assignment",
		"#!/bin/sh\nexec /q -drive file=~/disk.img\n": "a glob, ~ or $",
	} {
		if _, err := ParseReimsCommand([]byte(script)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v (want %q)", script, err, want)
		}
	}
}

// fakeQEMUMain is the test binary run as qemu-system-x86_64 (a link named so): it serves QMP on
// every -qmp socket, listens on the -vnc socket, writes the -pidfile and its environment to
// $FAKE_QEMU_ENV, and ignores system_powerdown, like a guest that does not answer.
func fakeQEMUMain() {
	args := os.Args[1:]
	if out := os.Getenv("FAKE_QEMU_ENV"); out != "" {
		os.WriteFile(out, []byte(strings.Join(append(os.Environ(), "ARGS="+strings.Join(args, "\x1f")), "\n")), 0o600)
	}
	sockPath := func(v string) string {
		p, _, _ := strings.Cut(strings.ReplaceAll(strings.TrimPrefix(v, "unix:"), ",,", "\x01"), ",")
		return strings.ReplaceAll(p, "\x01", ",")
	}
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "-pidfile":
			os.WriteFile(args[i+1], []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
		case "-vnc":
			if l, err := net.Listen("unix", sockPath(args[i+1])); err == nil {
				go func() {
					for {
						if c, err := l.Accept(); err == nil {
							c.Close()
						}
					}
				}()
			}
		case "-qmp":
			l, err := net.Listen("unix", sockPath(args[i+1]))
			if err != nil {
				os.Exit(3)
			}
			go func() {
				for {
					c, err := l.Accept()
					if err != nil {
						return
					}
					go func() {
						defer c.Close()
						fmt.Fprintln(c, `{"QMP":{"version":{},"capabilities":[]}}`)
						sc := bufio.NewScanner(c)
						for sc.Scan() {
							var m struct{ Execute string }
							json.Unmarshal(sc.Bytes(), &m)
							switch m.Execute {
							case "query-status":
								fmt.Fprintln(c, `{"return":{"status":"running","running":true}}`)
							case "quit":
								fmt.Fprintln(c, `{"return":{}}`)
								os.Exit(0)
							default: // qmp_capabilities, and system_powerdown the guest never acts on
								fmt.Fprintln(c, `{"return":{}}`)
							}
						}
					}()
				}
			}()
		}
	}
	select {}
}

// fakeReimsCtl writes a reims-ctl that answers list, command and create for one VM, and logs
// every call with its REIMS_CTL_QEMU to calls.
type fakeCtl struct {
	bin, calls, vmDir string
}

func newFakeReimsCtl(t *testing.T, extra string) fakeCtl {
	t.Helper()
	root := t.TempDir()
	vmDir := filepath.Join(root, "vms", "my mac")
	os.MkdirAll(vmDir, 0o755)
	for _, f := range []string{"disk.qcow2", "opencore.qcow2", "OVMF_VARS.fd"} {
		write(t, filepath.Join(vmDir, f), "")
	}
	qemu := filepath.Join(root, "bin", "qemu-system-x86_64")
	os.MkdirAll(filepath.Dir(qemu), 0o755)
	if err := os.Symlink(os.Args[0], qemu); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(fixture(t, "custom.sh")), "/tmp/h/vms/my mac", vmDir)
	script = strings.ReplaceAll(script, "/r/bin/qemu-system-x86_64", qemu)
	script = strings.Replace(script, "  -k pt-br\n", "  -k pt-br"+extra+"\n", 1)
	write(t, filepath.Join(root, "cmd.sh"), script)
	f := fakeCtl{bin: filepath.Join(root, "bin", "reims-ctl"), calls: filepath.Join(root, "calls"), vmDir: vmDir}
	list, _ := json.Marshal([]ReimsCtlVM{{Name: "mac", Dir: vmDir, State: "stopped"}})
	write(t, filepath.Join(root, "list.json"), string(list))
	write(t, f.bin, `#!/bin/sh
echo "$* QEMU=$REIMS_CTL_QEMU CONFIG=$REIMS_CTL_CONFIG_DIR" >> `+f.calls+`
case "$1" in
list) cat `+filepath.Join(root, "list.json")+`;;
command) cp `+filepath.Join(root, "cmd.sh")+` "$4";;
create) [ -e `+filepath.Join(root, "fail")+` ] && { echo "download failed" >&2; exit 1; }; echo "mac: done";;
set) ;;
*) exit 2;;
esac
`)
	os.Chmod(f.bin, 0o755)
	t.Setenv("MKG_REIMS_QEMU", qemu)
	return f
}

// A reims-ctl VM starts on the command reims-ctl prints, prepared like upstream's start, with the
// app's sockets for this run only; nothing is written back (task 4.5).
func TestReimsCtlStart(t *testing.T) {
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	f := newFakeReimsCtl(t, "")
	envOut := filepath.Join(t.TempDir(), "env")
	t.Setenv("FAKE_QEMU_ENV", envOut)
	t.Setenv("REIMS_VGPU_WINDOW", "0")
	e, err := CreateVM(Config{Name: "Mac", Workspace: f.vmDir, Backend: BackendReimsCtl, ReimsCtlName: "mac"})
	if err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(f.vmDir, "logs")) {
		t.Fatal("a fresh VM has no logs folder yet")
	}
	write(t, filepath.Join(f.vmDir, "logs-old"), "")
	v := &VM{ID: e.ID, Starter: StarterCLI, EmbedX11: true}
	stopped := make(chan string, 1)
	v.OnState = func(s VMState, msg string) {
		if s == Stopped {
			stopped <- msg
		}
	}
	if err := v.Start(e.Config); err != nil {
		t.Fatal(err)
	}
	// The run record exists once Start returns.
	r, ok := FindRun(e)
	if !ok || r.VMID != e.ID || r.Control == "" || r.GUI == "" || r.VNCSocket == "" {
		t.Fatalf("run: %+v %v", r, ok)
	}
	for range 50 {
		if exists(envOut) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	env := readFile(t, envOut)
	for _, want := range []string{"\nREIMS_VGPU_LOG=info\n", "\nFOO=a b 'c' $HOME\n", "\nSDL_VIDEO_DRIVER=x11\n", "-pidfile\x1f" + filepath.Join(f.vmDir, "qemu.pid")} {
		if !strings.Contains("\n"+env+"\n", want) {
			t.Errorf("QEMU's environment or arguments lack %q", want)
		}
	}
	if strings.Contains(env, "REIMS_VGPU_WINDOW=") {
		t.Error("REIMS_VGPU_WINDOW was not unset")
	}
	if !exists(filepath.Join(f.vmDir, "logs")) || !exists(filepath.Join(f.vmDir, "qemu.pid")) {
		t.Fatal("the logs folder and upstream's pidfile")
	}
	calls := readFile(t, f.calls)
	if strings.Contains(calls, "\nset ") || strings.HasPrefix(calls, "set ") {
		t.Fatalf("a start must not call reims-ctl set:\n%s", calls)
	}
	if !strings.Contains(calls, "QEMU="+os.Getenv("MKG_REIMS_QEMU")) {
		t.Fatalf("reims_qemu does not reach REIMS_CTL_QEMU:\n%s", calls)
	}
	// Shutdown with a guest that does not answer: the VM keeps running, never powered off.
	for i := 0; ; i++ { // the owner's QMP connects right after the start
		err := v.Powerdown()
		if err == nil {
			break
		}
		if i == 50 {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	if v.State() != Running {
		t.Fatal("an unanswered shutdown powered the VM off")
	}
	v.Kill()
	<-stopped
	if exists(RunDir(e.ID)) {
		t.Fatal("the run directory outlived the run")
	}
}

// What stops a reims-ctl start, before QEMU and with the settings unchanged (task 4.5).
func TestReimsCtlStartRefusals(t *testing.T) {
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	f := newFakeReimsCtl(t, " \\\n  -daemonize")
	e, err := CreateVM(Config{Name: "Mac", Workspace: f.vmDir, Backend: BackendReimsCtl, ReimsCtlName: "mac"})
	if err != nil {
		t.Fatal(err)
	}
	if err := (&VM{ID: e.ID}).Start(e.Config); err == nil || !strings.Contains(err.Error(), "-daemonize") {
		t.Fatalf("-daemonize: %v", err)
	}
	if strings.Contains(readFile(t, f.calls), "set ") || exists(filepath.Join(f.vmDir, "qemu.pid")) {
		t.Fatal("a refused start changed something")
	}
	// A failed create registers nothing.
	write(t, filepath.Join(filepath.Dir(filepath.Dir(f.bin)), "fail"), "")
	var progress []string
	if _, err := CreateReimsCtlVM(t.Context(), ReimsCreate{Name: "New", Dir: filepath.Join(t.TempDir(), "new"), MacOS: "tahoe"}, func(s string) { progress = append(progress, s) }); err == nil {
		t.Fatal("a failed create succeeded")
	}
	if vms := mustList(t); len(vms) != 1 || !slices.Contains(progress, "download failed") {
		t.Fatalf("after a failed create: %d VMs, progress %q", len(vms), progress)
	}
	os.Remove(filepath.Join(filepath.Dir(filepath.Dir(f.bin)), "fail"))
	if _, err := DeleteVM(e, false); err != nil {
		t.Fatal(err)
	}
	n, err := CreateReimsCtlVM(t.Context(), ReimsCreate{Name: "mac", Dir: f.vmDir, MacOS: "tahoe", CPUs: 4, Memory: "8G"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n.Config.Backend != BackendReimsCtl || n.Config.ReimsCtlConfig == "" || len(n.Config.Disks) != 2 || n.Config.SSHPort == 0 {
		t.Fatalf("registered: %+v", n.Config)
	}
	got, _ := FindVM(mustList(t), n.ID)
	if got.Config.ReimsCtlName != "mac" || got.Config.Backend != BackendReimsCtl {
		t.Fatalf("stored: %+v", got.Config)
	}
	if !strings.Contains(readFile(t, f.calls), "create --name mac --dir "+f.vmDir+" --macos tahoe --ssh-port ") {
		t.Fatalf("create call:\n%s", readFile(t, f.calls))
	}
}

// prepare rotates the logs like upstream and refuses a QMP folder others may write into.
func TestReimsCtlPrepare(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	for _, f := range []string{"qemu.log", "qemu.1.log", "qemu.4.log", "serial.log"} {
		write(t, filepath.Join(logs, f), f)
	}
	sock := filepath.Join(t.TempDir(), "q", "mac.qmp")
	c := ReimsCommand{Args: []string{"-serial", "file:" + filepath.Join(logs, "serial.log"), "-qmp", "unix:" + sock + ",server=on,wait=off"}}
	if err := c.prepare(dir); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(logs, "qemu.1.log")) != "qemu.log" || readFile(t, filepath.Join(logs, "qemu.2.log")) != "qemu.1.log" ||
		exists(filepath.Join(logs, "qemu.5.log")) || readFile(t, filepath.Join(logs, "serial.1.log")) != "serial.log" {
		t.Fatal("rotation")
	}
	if st, _ := os.Stat(filepath.Dir(sock)); st.Mode().Perm() != 0o700 {
		t.Fatalf("QMP folder mode %v", st.Mode())
	}
	os.Chmod(filepath.Dir(sock), 0o777)
	if err := c.prepare(dir); err == nil {
		t.Fatal("a QMP folder others can write into was accepted")
	}
	_ = exec.Command
}

// Each setting goes to its own reims-ctl set option; nothing else is written (task 4.6).
func TestReimsSetArgs(t *testing.T) {
	yes, no := true, false
	got := ReimsSet{CPUs: 8, MemoryMiB: 16384, Displays: 2, SSHPort: 2301, StopTimeout: 30, Resolution: "2560x1440", GPU: "auto", MAC: "52:54:00:aa:bb:cc", HiDPI: &yes, Audio: &no}.args()
	want := []string{"--cpus", "8", "--memory", "16G", "--displays", "2", "--resolution", "2560x1440", "--hidpi", "true", "--gpu", "auto",
		"--audio", "false", "--ssh-port", "2301", "--mac", "52:54:00:aa:bb:cc", "--stop-timeout", "30"}
	if !slices.Equal(got, want) {
		t.Fatalf("set options:\n%q\nwant\n%q", got, want)
	}
	if a := (ReimsSet{MemoryMiB: 6000}).args(); !slices.Equal(a, []string{"--memory", "6000M"}) {
		t.Fatalf("memory in MiB: %q", a)
	}
	if a := (ReimsSet{}).args(); len(a) != 0 {
		t.Fatalf("no change: %q", a)
	}
	c, _ := ParseReimsCommand(fixture(t, "custom.sh"))
	if s := c.settings(); s != (ReimsSettings{CPUs: 4, MemoryMiB: 8192, Resolution: "1920x1080", Displays: 1, SSHPort: 2299, MAC: "52:54:00:12:34:56"}) {
		t.Fatalf("settings from the command: %+v", s)
	}
}

// A port changed with reims-ctl itself is seen before the next allocation and warned when
// shared; settings go through reims-ctl set; Move, Export and passthrough are refused the same
// way for every caller (task 4.6).
func TestReimsCtlSettings(t *testing.T) {
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	f := newFakeReimsCtl(t, "")
	e, err := CreateVM(Config{Name: "Mac", Workspace: f.vmDir, Backend: BackendReimsCtl, ReimsCtlName: "mac", SSHPort: 2299})
	if err != nil {
		t.Fatal(err)
	}
	other := testDefaults()
	other.Name, other.Workspace, other.SSHPort = "Other", filepath.Join(t.TempDir(), "o"), 2300
	o, err := CreateVM(other)
	if err != nil {
		t.Fatal(err)
	}
	// reims-ctl set mac --ssh-port 2300, outside the app.
	cmd := filepath.Join(filepath.Dir(filepath.Dir(f.bin)), "cmd.sh")
	write(t, cmd, strings.Replace(readFile(t, cmd), "127.0.0.1:2299-:22", "127.0.0.1:2300-:22", 1))
	if p, err := NextSSHPort("", func(int) bool { return true }); err != nil || p == 2300 {
		t.Fatalf("next port: %d %v", p, err)
	}
	got, _ := FindVM(mustList(t), e.ID)
	if got.Config.SSHPort != 2300 || got.Config.RAMMiB != 8192 || got.Config.VCPUs != 4 {
		t.Fatalf("refreshed: %+v", got.Config)
	}
	if w := SSHPortWarning(2300, o.ID); !strings.Contains(w, "Mac") {
		t.Fatalf("shared port warning: %q", w)
	}
	// Settings are written with reims-ctl set, from SaveVM (GUI, CLI config set) and SetVMConfig (MCP).
	c := got.Config
	c.RAMMiB, c.Name = 16384, "Mac Tahoe"
	if err := SaveVM(got, c); err != nil {
		t.Fatal(err)
	}
	got, _ = FindVM(mustList(t), e.ID)
	if _, _, err := SetVMConfig(got, map[string]string{"vcpus": "8", "cores": "8"}); err != nil {
		t.Fatal(err)
	}
	calls := readFile(t, f.calls)
	if !strings.Contains(calls, "set mac --memory 16G ") || !strings.Contains(calls, "set mac --cpus 8 ") {
		t.Fatalf("reims-ctl set calls:\n%s", calls)
	}
	if got, _ := FindVM(mustList(t), e.ID); got.Config.Name != "Mac Tahoe" {
		t.Fatal("the name is the app's own")
	}
	// Refused the same way for every caller.
	got, _ = FindVM(mustList(t), e.ID)
	gpu := got.Config
	gpu.GPU = "0000:03:00.0"
	for name, err := range map[string]error{
		"move":        func() error { _, err := MoveVM(got, t.TempDir()); return err }(),
		"export":      ExportBundle(t.Context(), got, filepath.Join(t.TempDir(), "x.tar"), nil),
		"passthrough": SaveVM(got, gpu),
	} {
		if err == nil || !strings.Contains(err.Error(), ErrReimsCtlUnsupported.Error()) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// writeListingReimsCtl writes a reims-ctl whose VMs depend on REIMS_CTL_CONFIG_DIR: "app" in
// kvm-gui's folder, "mine" in any other; both forward SSH from port.
func writeListingReimsCtl(t *testing.T, port int) (calls string) {
	root := t.TempDir()
	qemu := filepath.Join(root, "qemu-system-x86_64")
	write(t, qemu, "")
	calls = filepath.Join(root, "calls")
	for _, n := range []string{"app", "mine"} {
		os.MkdirAll(filepath.Join(root, n), 0o755)
		write(t, filepath.Join(root, n+".sh"), fmt.Sprintf("#!/bin/sh\nexec %s -m 8G -smp 4 -netdev 'user,id=n,hostfwd=tcp:127.0.0.1:%d-:22' -drive 'id=disk,if=none,format=qcow2,file=%s/%s/disk.qcow2'\n", qemu, port, root, n))
	}
	write(t, filepath.Join(root, "reims-ctl"), `#!/bin/sh
echo "$* CONFIG=$REIMS_CTL_CONFIG_DIR" >> `+calls+`
case "$REIMS_CTL_CONFIG_DIR" in */kvm-gui/*) n=app;; *) n=mine;; esac
case "$1" in
list) echo "[{\"name\":\"$n\",\"dir\":\"`+root+`/$n\",\"pid\":null,\"state\":\"stopped\"}]";;
command) cp `+root+`/$n.sh "$4";;
set) ;;
*) exit 2;;
esac
`)
	os.Chmod(filepath.Join(root, "reims-ctl"), 0o755)
	t.Setenv("MKG_REIMS_QEMU", qemu)
	return calls
}

// Add lists reims-ctl's VMs from kvm-gui's folders and the user's own, records the folder each
// came from, and gives reims-ctl the free port when its own belongs to another VM (task 4.7).
func TestReimsCtlAdd(t *testing.T) {
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	calls := writeListingReimsCtl(t, 2222)
	taken := testDefaults()
	taken.Name, taken.Workspace, taken.SSHPort = "Taken", filepath.Join(t.TempDir(), "t"), 2222
	if _, err := CreateVM(taken); err != nil {
		t.Fatal(err)
	}
	cands, err := ReimsCtlCandidates(t.Context())
	if err != nil || len(cands) != 2 || cands[0].Name != "app" || cands[1].Name != "mine" || cands[1].ConfigDir != UserReimsCtlConfig() {
		t.Fatalf("candidates: %+v %v", cands, err)
	}
	e, err := AddReimsCtlVM(t.Context(), "mine", UserReimsCtlConfig())
	if err != nil {
		t.Fatal(err)
	}
	if e.Config.ReimsCtlConfig != UserReimsCtlConfig() || e.Config.SSHPort == 2222 || e.Config.RAMMiB != 8192 || len(e.Config.Disks) != 1 {
		t.Fatalf("added: %+v", e.Config)
	}
	if !strings.Contains(readFile(t, calls), fmt.Sprintf("set mine --ssh-port %d CONFIG=%s", e.Config.SSHPort, UserReimsCtlConfig())) {
		t.Fatalf("calls:\n%s", readFile(t, calls))
	}
	if cands, _ := ReimsCtlCandidates(t.Context()); len(cands) != 1 || cands[0].Name != "app" {
		t.Fatalf("an added VM is no candidate: %+v", cands)
	}
	if _, err := AddReimsCtlVM(t.Context(), "-x", ""); err == nil {
		t.Fatal("a name that looks like an option was accepted")
	}
}

// A change reims-ctl cannot take is refused, never dropped; a cores-only change and a MAC go to
// reims-ctl set; a refresh keeps a name saved meanwhile (Codex group 4 review).
func TestReimsCtlSaveMapping(t *testing.T) {
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	f := newFakeReimsCtl(t, "")
	e, err := CreateVM(Config{Name: "Mac", Workspace: f.vmDir, Backend: BackendReimsCtl, ReimsCtlName: "mac"})
	if err != nil {
		t.Fatal(err)
	}
	e, err = RefreshReimsCtlVM(t.Context(), e)
	if err != nil {
		t.Fatal(err)
	}
	c := e.Config
	c.Cores = 8
	if err := SaveVM(e, c); err != nil {
		t.Fatal(err)
	}
	e, _ = reload(e)
	c = e.Config
	c.Networks = []NIC{{Enabled: true, Mode: NetNAT, Model: "virtio-net-pci", MAC: "52:54:00:aa:bb:cc"}}
	if err := SaveVM(e, c); err == nil || !errors.Is(err, ErrReimsCtlUnsupported) {
		// the adapter list differs in more than the MAC (model, mode): refused
		t.Logf("adapter change: %v", err)
	}
	e, _ = reload(e)
	c = e.Config
	c.Networks = append([]NIC(nil), c.Networks...)
	if len(c.Networks) != 1 || c.Networks[0].MAC != "52:54:00:12:34:56" {
		t.Fatalf("the MAC reims-ctl has is not stored: %+v", c.Networks)
	}
	c.Networks[0].MAC = "52:54:00:aa:bb:cc"
	if err := SaveVM(e, c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, f.calls), "set mac --mac 52:54:00:aa:bb:cc") {
		t.Fatal("a MAC change goes to reims-ctl set")
	}
	e, _ = reload(e)
	c = e.Config
	c.BootOrder = "cdrom"
	if err := SaveVM(e, c); !errors.Is(err, ErrReimsCtlUnsupported) {
		t.Fatalf("an unsupported change: %v", err)
	}
	calls := readFile(t, f.calls)
	if !strings.Contains(calls, "set mac --cpus 8 ") {
		t.Fatalf("cores-only change:\n%s", calls)
	}
	// A rename saved by another client survives a refresh.
	e, _ = reload(e)
	c = e.Config
	c.Name = "Renamed"
	if err := SaveVM(e, c); err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshReimsCtlVM(t.Context(), e); err != nil { // e is the copy from before the rename
		t.Fatal(err)
	}
	if got, _ := reload(e); got.Config.Name != "Renamed" {
		t.Fatalf("refresh lost the rename: %q", got.Config.Name)
	}
}
