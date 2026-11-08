package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// BackendReimsCtl is Config.Backend for a macOS VM that upstream's reims-ctl created and keeps:
// the app runs the command reims-ctl prints for it (design D19).
const BackendReimsCtl = "reims-ctl"

// ReimsCtl runs upstream's reims-ctl, a separate program, on one configuration folder.
type ReimsCtl struct {
	Bin       string // the reims-ctl binary
	ConfigDir string // REIMS_CTL_CONFIG_DIR: its vms.toml
	DataDir   string // REIMS_CTL_DATA_DIR ("" with ConfigDir: upstream's default)
	QEMU      string // REIMS_CTL_QEMU: the Reims QEMU the app checked
}

// reimsCtlFor is the reims-ctl for c: kvm-gui's own folders, or the configuration folder c was
// added from (the user's own reims-ctl).
func reimsCtlFor(c Config) (ReimsCtl, error) {
	qemu, err := ReimsQEMU()
	if err != nil {
		return ReimsCtl{}, err
	}
	r := ReimsCtl{QEMU: qemu, ConfigDir: filepath.Join(ConfigDir(), "reims-ctl"), DataDir: filepath.Join(DataDir(), "reims-ctl")}
	if c.ReimsCtlConfig != "" && c.ReimsCtlConfig != r.ConfigDir {
		r.ConfigDir, r.DataDir = c.ReimsCtlConfig, ""
	}
	r.Bin = filepath.Join(filepath.Dir(qemu), "reims-ctl") // the package's bin/ holds both
	if _, err := os.Stat(r.Bin); err != nil {
		if r.Bin, err = exec.LookPath("reims-ctl"); err != nil {
			return ReimsCtl{}, fmt.Errorf("reims-ctl is not installed next to %s or on PATH: install Reims vGPU", qemu)
		}
	}
	return r, nil
}

func (r ReimsCtl) env() []string {
	env := slices.DeleteFunc(os.Environ(), func(e string) bool {
		k, _, _ := strings.Cut(e, "=")
		return k == "REIMS_CTL_CONFIG_DIR" || k == "REIMS_CTL_DATA_DIR" || k == "REIMS_CTL_QEMU" || k == "XDG_CACHE_HOME"
	})
	env = append(env, "REIMS_CTL_CONFIG_DIR="+r.ConfigDir, "REIMS_CTL_QEMU="+r.QEMU)
	if r.DataDir != "" { // kvm-gui's own folders; a user's reims-ctl keeps its own data and cache
		env = append(env, "REIMS_CTL_DATA_DIR="+r.DataDir, "XDG_CACHE_HOME="+CacheDir())
	} else if v, ok := os.LookupEnv("XDG_CACHE_HOME"); ok {
		env = append(env, "XDG_CACHE_HOME="+v)
	}
	return env
}

// run runs reims-ctl, each output line to progress (nil: collected for the error only).
func (r ReimsCtl) run(ctx context.Context, progress func(string), args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	cmd.Env = r.env()
	var out bytes.Buffer
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			out.WriteString(sc.Text() + "\n")
			if progress != nil {
				progress(sc.Text())
			}
		}
	}()
	err := cmd.Run()
	pw.Close()
	<-done
	if err != nil {
		return out.Bytes(), fmt.Errorf("reims-ctl %s: %v: %s", args[0], err, strings.TrimSpace(out.String()))
	}
	return out.Bytes(), nil
}

// ReimsCtlVM is one entry of `reims-ctl list --json`.
type ReimsCtlVM struct {
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	PID   *int   `json:"pid"`
	State string `json:"state"`
}

// List is `reims-ctl list --json`.
func (r ReimsCtl) List(ctx context.Context) ([]ReimsCtlVM, error) {
	cmd := exec.CommandContext(ctx, r.Bin, "list", "--json")
	cmd.Env = r.env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reims-ctl list: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var vms []ReimsCtlVM
	if err := json.Unmarshal(out, &vms); err != nil {
		return nil, fmt.Errorf("reims-ctl list --json: %w", err)
	}
	return vms, nil
}

func (r ReimsCtl) find(ctx context.Context, name string) (ReimsCtlVM, error) {
	vms, err := r.List(ctx)
	if err != nil {
		return ReimsCtlVM{}, err
	}
	for _, v := range vms {
		if v.Name == name {
			return v, nil
		}
	}
	return ReimsCtlVM{}, fmt.Errorf("reims-ctl has no VM named %s in %s", name, r.ConfigDir)
}

// Logs is `reims-ctl logs`: the end of the VM's QEMU log, or its serial console's.
func (r ReimsCtl) Logs(ctx context.Context, name string, serial bool) (string, error) {
	args := []string{"logs", name, "--lines", "200"}
	if serial {
		args = append(args, "--serial")
	}
	out, err := r.run(ctx, nil, args...)
	return string(out), err
}

// Insert gives the VM an installer image for its next start; Eject takes it out. For a
// reims-ctl VM the installer is a disk, not a removable drive.
func (r ReimsCtl) Insert(ctx context.Context, name, image string) error {
	_, err := r.run(ctx, nil, "insert", name, image)
	return err
}

func (r ReimsCtl) Eject(ctx context.Context, name string) error {
	_, err := r.run(ctx, nil, "eject", name)
	return err
}

// ReimsCreate is the wizard's `reims-ctl create`.
type ReimsCreate struct {
	Name, Dir, MacOS string
	Offline          bool
	CPUs             int
	Memory, DiskSize string // such as 8G, 128G
	MAC              string
}

// CreateReimsCtlVM runs `reims-ctl create mac` with kvm-gui's folders, its output as progress,
// and registers the VM with a free SSH port only once reims-ctl made it: a failed create
// registers nothing.
func CreateReimsCtlVM(ctx context.Context, o ReimsCreate, progress func(string)) (VMEntry, error) {
	c := Config{Name: o.Name, Workspace: o.Dir, Version: o.MacOS, Backend: BackendReimsCtl, ReimsCtlName: Slug(o.Name)}
	if err := c.validateReimsCtl(); err != nil {
		return VMEntry{}, err
	}
	r, err := reimsCtlFor(c)
	if err != nil {
		return VMEntry{}, err
	}
	port, err := NextSSHPort("", nil)
	if err != nil {
		return VMEntry{}, err
	}
	args := []string{"create", "--name", c.ReimsCtlName, "--dir", o.Dir, "--macos", o.MacOS, "--ssh-port", fmt.Sprint(port)}
	if o.Offline {
		args = append(args, "--offline")
	}
	for k, v := range map[string]string{"--memory": o.Memory, "--disk-size": o.DiskSize, "--mac": o.MAC} {
		if v != "" {
			args = append(args, k, v)
		}
	}
	if o.CPUs > 0 {
		args = append(args, "--cpus", fmt.Sprint(o.CPUs))
	}
	if _, err := r.run(ctx, progress, args...); err != nil {
		return VMEntry{}, err
	}
	c.SSHPort, c.ReimsCtlConfig = port, r.ConfigDir
	if err := refreshReimsCtl(ctx, r, &c); err != nil {
		return VMEntry{}, err
	}
	e, err := CreateVM(c)
	if err != nil {
		return VMEntry{}, err
	}
	if e.Config.SSHPort != port { // another VM took the port meanwhile: give reims-ctl the new one
		if _, err := r.run(ctx, nil, "set", c.ReimsCtlName, "--ssh-port", fmt.Sprint(e.Config.SSHPort)); err != nil {
			return e, err
		}
	}
	return e, nil
}

// refreshReimsCtl reads c's folder and disks from reims-ctl's current command, so a VM moved or
// changed with reims-ctl itself is found by its disks.
func refreshReimsCtl(ctx context.Context, r ReimsCtl, c *Config) error {
	v, err := r.find(ctx, c.ReimsCtlName)
	if err != nil {
		return err
	}
	cmd, err := r.command(ctx, c.ReimsCtlName, "")
	if err != nil {
		return err
	}
	c.Workspace, c.Disks = v.Dir, nil
	for _, f := range cmd.driveFiles() {
		c.Disks = append(c.Disks, Disk{Path: f, Format: "qcow2", Bus: BusSATA})
	}
	return nil
}

// command runs `reims-ctl command` into path (a temporary file when "") and parses it.
func (r ReimsCtl) command(ctx context.Context, name, path string) (ReimsCommand, error) {
	if path == "" {
		f, err := os.CreateTemp("", "reims-cmd-*.sh")
		if err != nil {
			return ReimsCommand{}, err
		}
		f.Close()
		defer os.Remove(f.Name())
		path = f.Name()
	}
	if _, err := r.run(ctx, nil, "command", name, "-o", path); err != nil {
		return ReimsCommand{}, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return ReimsCommand{}, err
	}
	return ParseReimsCommand(src)
}

// ReimsCommand is the QEMU command `reims-ctl command` prints, read without running it.
type ReimsCommand struct {
	Unset   []string // variables removed from QEMU's environment
	Env     []string // KEY=VALUE added to it
	Program string
	Args    []string
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseReimsCommand reads the script upstream's shell_script() emits with a POSIX shell parser:
// `unset NAME`, `export NAME=word`, then one `exec program args...`, every word literal (quotes
// and backslashes only). Anything else (another command, an expansion, a glob, a redirection) is
// refused, naming it, and nothing of the script is ever run.
func ParseReimsCommand(src []byte) (ReimsCommand, error) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(bytes.NewReader(src), "reims-cmd.sh")
	if err != nil {
		return ReimsCommand{}, fmt.Errorf("reims-ctl's command: %w", err)
	}
	var cmd ReimsCommand
	refuse := func(n syntax.Node, what string) error {
		return fmt.Errorf("reims-ctl's command, line %d: %s: the app runs only unset, export and exec with literal words", n.Pos().Line(), what)
	}
	var bad error
	syntax.Walk(f, func(n syntax.Node) bool {
		if bad != nil || n == nil { // nil: Walk's end of a node's children
			return bad == nil
		}
		switch n := n.(type) {
		case *syntax.File, *syntax.Stmt, *syntax.CallExpr, *syntax.DeclClause, *syntax.Assign, *syntax.Word, *syntax.Comment, *syntax.Lit:
		case *syntax.SglQuoted:
			if n.Dollar {
				bad = refuse(n, "a $'...' string")
			}
		case *syntax.DblQuoted:
			if n.Dollar {
				bad = refuse(n, `a $"..." string`)
			}
			for _, p := range n.Parts {
				if _, ok := p.(*syntax.Lit); !ok {
					bad = refuse(p, "an expansion inside double quotes")
				}
			}
		default:
			bad = refuse(n, fmt.Sprintf("%T", n))
		}
		return bad == nil
	})
	if bad != nil {
		return ReimsCommand{}, bad
	}
	lit := func(w *syntax.Word) (string, error) {
		for _, p := range w.Parts { // unquoted parts only: inside quotes these are literal
			if l, ok := p.(*syntax.Lit); ok && strings.ContainsAny(l.Value, "*?[~$") {
				return "", refuse(w, "the word "+l.Value+" would be expanded (a glob, ~ or $)")
			}
		}
		return expand.Literal(nil, w)
	}
	for i, st := range f.Stmts {
		if st.Negated || st.Background || st.Coprocess || len(st.Redirs) > 0 {
			return ReimsCommand{}, refuse(st, "a redirection, a background or negated command")
		}
		if cmd.Program != "" {
			return ReimsCommand{}, refuse(st, "a statement after exec")
		}
		switch c := st.Cmd.(type) {
		case *syntax.DeclClause:
			if c.Variant.Value != "export" {
				return ReimsCommand{}, refuse(st, c.Variant.Value)
			}
			for _, a := range c.Args {
				if a.Naked || a.Append || a.Index != nil || a.Array != nil || a.Value == nil || !envName.MatchString(a.Name.Value) {
					return ReimsCommand{}, refuse(a, "export of something other than NAME=word")
				}
				v, err := lit(a.Value)
				if err != nil {
					return ReimsCommand{}, err
				}
				cmd.Env = append(cmd.Env, a.Name.Value+"="+v)
			}
		case *syntax.CallExpr:
			if len(c.Assigns) > 0 || len(c.Args) == 0 {
				return ReimsCommand{}, refuse(st, "an assignment")
			}
			var words []string
			for _, w := range c.Args {
				s, err := lit(w)
				if err != nil {
					return ReimsCommand{}, err
				}
				words = append(words, s)
			}
			switch words[0] {
			case "export": // POSIX parsing gives export as a plain command: NAME=word
				for _, kv := range words[1:] {
					if k, _, ok := strings.Cut(kv, "="); !ok || !envName.MatchString(k) {
						return ReimsCommand{}, refuse(st, "export of something other than NAME=word")
					}
				}
				cmd.Env = append(cmd.Env, words[1:]...)
			case "unset":
				for _, k := range words[1:] {
					if !envName.MatchString(k) {
						return ReimsCommand{}, refuse(st, "unset "+k)
					}
				}
				cmd.Unset = append(cmd.Unset, words[1:]...)
			case "exec":
				if len(words) < 2 || !filepath.IsAbs(words[1]) || i != len(f.Stmts)-1 {
					return ReimsCommand{}, refuse(st, "exec without an absolute program, or not last")
				}
				cmd.Program, cmd.Args = words[1], words[2:]
			default:
				return ReimsCommand{}, refuse(st, "the command "+words[0])
			}
		default:
			return ReimsCommand{}, refuse(st, fmt.Sprintf("%T", st.Cmd))
		}
	}
	if cmd.Program == "" {
		return ReimsCommand{}, errors.New("reims-ctl's command has no exec of QEMU")
	}
	return cmd, nil
}

// option is an argument's QEMU option name ("-name" and "--name" alike), "" for a value.
func option(a string) string {
	if !strings.HasPrefix(a, "-") {
		return ""
	}
	return "-" + strings.TrimLeft(a, "-")
}

// value is the value after option o ("" when it is missing).
func (c ReimsCommand) values(o string) []string {
	var vs []string
	for i, a := range c.Args {
		if option(a) == o && i+1 < len(c.Args) {
			vs = append(vs, c.Args[i+1])
		}
	}
	return vs
}

// suboption is key's value in a QEMU option string ("a=1,b=2", with ",," for a comma).
func suboption(s, key string) string {
	for _, p := range strings.Split(strings.ReplaceAll(s, ",,", "\x01"), ",") {
		if v, ok := strings.CutPrefix(p, key+"="); ok {
			return strings.ReplaceAll(v, "\x01", ",")
		}
	}
	return ""
}

// driveFiles are the VM's own images (system disk, OpenCore, firmware variables), without the
// read-only and snapshot ones (the code firmware, the installer).
func (c ReimsCommand) driveFiles() []string {
	var files []string
	for _, d := range c.values("-drive") {
		if f := suboption(d, "file"); f != "" && suboption(d, "readonly") != "on" && suboption(d, "snapshot") != "on" && suboption(d, "if") != "pflash" {
			files = append(files, f)
		}
	}
	return files
}

// check refuses options that would detach QEMU or take it from its owner (design D19), and any
// path in kvm-gui's run folder.
func (c ReimsCommand) check(vmDir string) error {
	pidfiles := 0
	runs := filepath.Join(runBase(), appDirName) + string(filepath.Separator)
	for i, a := range c.Args {
		switch option(a) {
		case "-daemonize", "-runas", "-chroot", "-run-with", "-vnc":
			return fmt.Errorf("reims-ctl's command has %s, which the app cannot run (it would detach QEMU or change its owner): remove it from the VM's extra_args with reims-ctl set", a)
		case "-pidfile":
			pidfiles++
			if i+1 >= len(c.Args) || c.Args[i+1] != filepath.Join(vmDir, "qemu.pid") || pidfiles > 1 {
				return errors.New("reims-ctl's command has a -pidfile other than the VM's own qemu.pid: remove it from the VM's extra_args with reims-ctl set")
			}
		}
		if strings.Contains(a, runs) {
			return fmt.Errorf("reims-ctl's command uses a path in the app's run folder (%s)", runs)
		}
	}
	return nil
}

// qmpSocket is upstream's own QMP socket from the command ("" without one).
func (c ReimsCommand) qmpSocket() string {
	for _, q := range c.values("-qmp") {
		if p, ok := strings.CutPrefix(q, "unix:"); ok {
			p, _, _ = strings.Cut(strings.ReplaceAll(p, ",,", "\x01"), ",")
			return strings.ReplaceAll(p, "\x01", ",")
		}
	}
	return ""
}

// prepare does what upstream's prepare_run does before QEMU starts: the logs folder, rotated
// qemu.log and (logs on) serial.log, and a private folder for its QMP socket.
func (c ReimsCommand) prepare(vmDir string) error {
	logs := filepath.Join(vmDir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		return err
	}
	rotateLog(filepath.Join(logs, "qemu.log"))
	for _, s := range c.values("-serial") {
		if s == "file:"+filepath.Join(logs, "serial.log") {
			rotateLog(filepath.Join(logs, "serial.log"))
		}
	}
	if sock := c.qmpSocket(); sock != "" && filepath.Dir(sock) != vmDir {
		d := filepath.Dir(sock)
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		st, err := os.Lstat(d)
		if err != nil {
			return err
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !st.IsDir() || !ok || int(sys.Uid) != os.Getuid() || st.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%s is not a folder of this user's that others cannot write into, so it holds no QMP socket: remove it, or make it so", d)
		}
	}
	return nil
}

// rotateLog keeps name.log as name.1.log, and so on up to name.4.log.
func rotateLog(path string) {
	stem := strings.TrimSuffix(path, ".log")
	nth := func(n int) string { return fmt.Sprintf("%s.%d.log", stem, n) }
	os.Remove(nth(4))
	for n := 3; n >= 1; n-- {
		os.Rename(nth(n), nth(n+1))
	}
	os.Rename(path, nth(1))
}

// environ is QEMU's environment: this process's without the unset names, plus the exports.
func (c ReimsCommand) environ(base []string) []string {
	out := slices.DeleteFunc(slices.Clone(base), func(e string) bool {
		k, _, _ := strings.Cut(e, "=")
		return slices.Contains(c.Unset, k) || slices.ContainsFunc(c.Env, func(x string) bool { return strings.HasPrefix(x, k+"=") })
	})
	return append(out, c.Env...)
}

// validateReimsCtl checks a reims-ctl VM's own fields; its settings are reims-ctl's.
func (c Config) validateReimsCtl() error {
	switch {
	case !c.MacOS():
		return &FieldError{"backend", "reims-ctl runs macOS VMs only"}
	case c.ReimsCtlName == "" || strings.ContainsAny(c.ReimsCtlName, "/\\ ") || strings.HasPrefix(c.ReimsCtlName, "-"):
		return &FieldError{"reims_ctl_name", "a reims-ctl VM name: letters, digits, '.', '_' and '-'"}
	case c.ReimsCtlConfig != "" && !filepath.IsAbs(c.ReimsCtlConfig):
		return &FieldError{"reims_ctl_config", "an absolute folder"}
	}
	return nil
}

// startReimsCtl starts a reims-ctl VM: the command reims-ctl prints now, checked and prepared like
// upstream's start, run by this process with the app's VNC and QMP sockets added for this run
// only. Nothing is written back with reims-ctl set.
func (v *VM) startReimsCtl(c Config) error {
	ctx := context.Background()
	r, err := reimsCtlFor(c)
	if err != nil {
		return err
	}
	if err := refreshReimsCtl(ctx, r, &c); err != nil {
		return err
	}
	dir := c.Workspace
	if r, ok := v.findRun(c); ok {
		return fmt.Errorf("this VM's disk is already in use by QEMU (pid %d): shut that VM down first", r.PID)
	}
	unlock, err := v.lockRun(NewWorkspace(dir), c)
	if err != nil {
		return err
	}
	runDir, err := v.newRunDir()
	if err != nil {
		unlock()
		return err
	}
	fail := func(err error) error { os.RemoveAll(runDir); unlock(); return err }
	cmd, err := r.command(ctx, c.ReimsCtlName, filepath.Join(runDir, "reims-cmd.sh"))
	if err != nil {
		return fail(err)
	}
	if err := cmd.check(dir); err != nil {
		return fail(err)
	}
	if err := cmd.prepare(dir); err != nil {
		return fail(err)
	}
	sock := filepath.Join(runDir, "qmp.sock")
	args := append(slices.Clone(cmd.Args), "-vnc", "unix:"+qemuPath(VNCSocket(sock))+",lock-key-sync=off")
	args = append(args, qmpArgs(sock)...)
	env := reimsEnv(cmd.environ(os.Environ()), v.EmbedX11)
	v.log(cmd.Program + " " + strings.Join(args, " "))
	return v.launch(launchSpec{qemu: cmd.Program, args: args, dir: dir, env: env, sock: sock, qmpDir: runDir, vnc: true, unlock: unlock})
}
