package core

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// Starters of a run, recorded in run.json.
const (
	StarterGUI = "gui"
	StarterCLI = "cli"
	StarterMCP = "mcp"
)

// RunRecord is run.json, written by the process that owns a VM's QEMU in the VM's private run
// directory. The app controls a run only through its record, checked against /proc (same pid and
// start time), so a reused pid or a shared disk never makes another run its own (design D4).
type RunRecord struct {
	VMID       string `json:"vm_id"`
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"` // field 22 of /proc/<pid>/stat
	Generation string `json:"generation"`
	Starter    string `json:"starter"`
}

// runBase is $XDG_RUNTIME_DIR (or the temporary folder when it is unset).
func runBase() string {
	if b := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(b) {
		return b
	}
	return os.TempDir()
}

// RunDir is a VM's private run directory: $XDG_RUNTIME_DIR/kvm-gui/<key>, the key being the id
// when it is short, else a prefix and a hash, so QEMU's socket paths stay under the 108-byte
// limit for any VM name. run.json keeps the full id.
func RunDir(vmID string) string { return filepath.Join(runBase(), appDirName, runKey(vmID)) }

func runKey(id string) string {
	if len(id) <= 24 {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return id[:12] + "-" + hex.EncodeToString(sum[:6])
}

// ConfigFileID is the run identity of a VM run from a --config settings file: one per file.
func ConfigFileID(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		if r, err := filepath.EvalSymlinks(abs); err == nil {
			abs = r
		}
	}
	sum := sha256.Sum256([]byte(abs))
	return "config-" + hex.EncodeToString(sum[:8])
}

// makeRunDir creates the VM's run directory (and its parent) 0700, owned by this user, and
// empties what a previous run left there.
func makeRunDir(vmID string) (string, error) {
	if vmID == "" || strings.ContainsAny(vmID, "/\\") || vmID == "." || vmID == ".." {
		return "", fmt.Errorf("invalid VM id %q", vmID)
	}
	parent := filepath.Join(runBase(), appDirName)
	for _, d := range []string{parent, RunDir(vmID)} {
		if err := os.Mkdir(d, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		if err := privateDir(d); err != nil {
			return "", err
		}
	}
	dir := RunDir(vmID)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(dir, e.Name()))
	}
	return dir, nil
}

// privateDir refuses a directory that is a link, not this user's, or open to others.
func privateDir(d string) error {
	st, err := os.Lstat(d)
	if err != nil {
		return err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || !ok || int(sys.Uid) != os.Getuid() || st.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s is not a private directory of this user (0700)", d)
	}
	return nil
}

// startTicks is a process's start time in clock ticks since boot (/proc/<pid>/stat field 22).
func startTicks(proc string, pid int) (uint64, bool) {
	data, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	// The command name (field 2) may hold spaces and parentheses: count from the last ')'.
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(string(data[i+1:]))
	if len(f) < 20 {
		return 0, false
	}
	t, err := strconv.ParseUint(f[19], 10, 64) // field 22 overall; f[0] is field 3
	return t, err == nil
}

// writeRunRecord writes run.json for a QEMU just started.
func writeRunRecord(dir, vmID, starter string, pid int) error {
	ticks, _ := startTicks("/proc", pid)
	g := make([]byte, 8)
	rand.Read(g)
	data, _ := json.Marshal(RunRecord{VMID: vmID, PID: pid, StartTicks: ticks, Generation: hex.EncodeToString(g), Starter: starter})
	return writeFileAtomic(filepath.Join(dir, "run.json"), data, 0o600)
}

// readRunRecord reads a VM's run.json and checks that its process still runs with the same start
// time; a stale or foreign record gives ok false.
func readRunRecord(proc, vmID string) (RunRecord, bool) {
	r, ok := readRunRecordAt(proc, RunDir(vmID))
	return r, ok && r.VMID == vmID
}

// readRunRecordAt reads the run.json in dir, checked against /proc.
func readRunRecordAt(proc, dir string) (RunRecord, bool) {
	if privateDir(dir) != nil {
		return RunRecord{}, false
	}
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return RunRecord{}, false
	}
	var r RunRecord
	if json.Unmarshal(data, &r) != nil || r.VMID == "" || filepath.Base(dir) != runKey(r.VMID) || r.PID <= 0 {
		return RunRecord{}, false
	}
	if t, ok := startTicks(proc, r.PID); !ok || t != r.StartTicks {
		return RunRecord{}, false
	}
	return r, true
}

// FindRun is the VM's run as the app may control it. A QEMU found through one of the VM's disks
// whose run record names another VM marks e busy (Busy, OtherVM) but carries no socket, so a
// shared disk never grants control of another VM's run. A VM without a disk (a live ISO) is found
// through its run record.
func FindRun(e VMEntry) (RunInfo, bool) { return findRunIn("/proc", e) }

func findRunIn(proc string, e VMEntry) (RunInfo, bool) {
	r, ok := runningIn(proc, e.Config)
	if !ok && e.ID != "" {
		rec, ok := readRunRecord(proc, e.ID)
		if !ok {
			return RunInfo{}, false
		}
		info, found := processInfo(proc, rec.PID)
		if !found {
			return RunInfo{}, false
		}
		info.VMID, info.Starter = rec.VMID, rec.Starter
		return info, true
	}
	if !ok {
		return RunInfo{}, false
	}
	if r.VMID != e.ID {
		// Another VM's run, or a QEMU without a valid record (started outside the app, or by an
		// older version): the VM is busy, but nothing of that run is this VM's to control.
		return RunInfo{PID: r.PID, Started: r.Started, OtherVM: r.VMID, ByApp: r.ByApp, Reims: r.Reims, SSHPort: r.SSHPort}, true
	}
	return r, true
}

// lockImages takes one lock per disk image, by identity (device, inode) and in that order, for the
// whole run: two VMs that share an image, through any path or link, cannot both pass the busy
// check and start (design D4). The locks are files in the runtime folder, never the images.
func lockImages(c Config) (func(), error) {
	type id struct{ dev, ino uint64 }
	var ids []id
	for _, st := range diskFiles(c) {
		if s, ok := st.Sys().(*syscall.Stat_t); ok {
			ids = append(ids, id{uint64(s.Dev), s.Ino})
		}
	}
	slices.SortFunc(ids, func(a, b id) int { return cmp.Or(cmp.Compare(a.dev, b.dev), cmp.Compare(a.ino, b.ino)) })
	ids = slices.Compact(ids)
	var files []*os.File
	release := func() {
		for _, f := range files {
			f.Close()
		}
	}
	for _, i := range ids {
		f, err := os.OpenFile(filepath.Join(runBase(), fmt.Sprintf("kvm-gui-image-%x-%x.lock", i.dev, i.ino)), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			release()
			return nil, err
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			release()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return nil, fmt.Errorf("a disk image of this VM is in use by another VM's start or run: %w", ErrBusy)
			}
			return nil, err
		}
		files = append(files, f)
	}
	return release, nil
}

// lockRun takes the VM's folder lock and its image locks, then checks again under them that no
// QEMU has the VM's disks open; unlock releases them all.
func (v *VM) lockRun(w Workspace, c Config) (func(), error) {
	unlock, err := lockRunRaw(v, w, c)
	if err != nil || v.ID == "" {
		return unlock, err
	}
	// An interrupted snapshot operation is finished or undone under these same locks, so nothing
	// can use a half-restored VM in between; the caller's settings may be stale then: it retries.
	did, err := recoverLocked(VMEntry{ID: v.ID, Config: c}, w.Dir)
	if err != nil {
		unlock()
		return nil, fmt.Errorf("an interrupted snapshot operation could not be completed: %w", err)
	}
	if did {
		unlock()
		return nil, errors.New("an interrupted snapshot operation of this VM was just completed: try again")
	}
	return unlock, nil
}

// lockRunRaw takes the locks of lockRun without the snapshot recovery.
func lockRunRaw(v *VM, w Workspace, c Config) (func(), error) {
	unlockDir, err := w.Lock()
	if err != nil {
		return nil, err
	}
	unlockImages, err := lockImages(c)
	if err != nil {
		unlockDir()
		return nil, err
	}
	unlock := func() { unlockImages(); unlockDir() }
	if r, ok := v.findRun(c); ok {
		unlock()
		return nil, fmt.Errorf("this VM's disk is already in use by QEMU (pid %d): shut that VM down first", r.PID)
	}
	return unlock, nil
}
