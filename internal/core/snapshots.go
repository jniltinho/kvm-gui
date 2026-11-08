package core

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// Snapshots are offline qcow2 internal snapshots of every hard disk plus copies of the VM's
// UEFI variables and TPM state, recorded in the snapshots table; take, restore and delete are
// journaled so an interrupted one is finished or undone the next time the VM is opened (design D10).

// Snapshot is one node of a VM's snapshot tree.
type Snapshot struct {
	ID          string       `json:"id"`
	ParentID    string       `json:"parent_id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	CreatedAt   string       `json:"created_at"`
	Manifest    snapManifest `json:"-"`
	state       string       // "deleting" once a delete has started
}

// snapManifest is what a snapshot holds: its disks (as the VM's config names them: relative to
// the VM folder or absolute), the state files copied (relative to the VM folder) and the settings.
type snapManifest struct {
	Disks  []string `json:"disks"`
	Files  []string `json:"files,omitempty"`
	Config Config   `json:"config"`
}

const snapDir = "snapshots"

// snapStep runs before each step of an operation; tests fail or "kill" an operation there.
var snapStep = func(op, step string) error { return nil }

// errKilled, returned by a test's snapStep, stops an operation where it is, like a killed process.
var errKilled = errors.New("killed")

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func snapTag(id string) string { return "kvm-gui-" + id }
func preTag(op string) string  { return "kvm-gui-pre-" + op }

// snapshotDisks are the VM's hard disks as its config names them; install media are not disks.
func snapshotDisks(c Config) []string {
	if c.MacOS() {
		return []string{DiskFile}
	}
	var out []string
	for _, d := range c.Disks {
		out = append(out, d.Path)
	}
	return out
}

// stateFiles are the files besides the disks a snapshot keeps, relative to the VM folder.
func stateFiles(c Config) []string {
	if c.MacOS() {
		return []string{VarsFile}
	}
	return []string{GuestVarsFile, GuestVarsKind, GuestTPMDir}
}

func diskAbs(dir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, p)
}

func isQcow2(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil {
		return false, nil
	}
	return string(magic) == "QFI\xfb", nil
}

// checkSnapshotEligible refuses a reims-ctl VM, a raw or missing disk, and a disk another VM or
// another VM's snapshot uses.
func checkSnapshotEligible(e VMEntry) error {
	if err := refuseReimsCtl(e.Config, "Snapshots"); err != nil {
		return err
	}
	dir, err := e.Config.WorkspaceDir()
	if err != nil {
		return err
	}
	var raw []string
	mine := map[string]os.FileInfo{}
	for _, d := range snapshotDisks(e.Config) {
		p := diskAbs(dir, d)
		q, err := isQcow2(p)
		if err != nil {
			return fmt.Errorf("disk %s: %w", p, err)
		}
		if !q {
			raw = append(raw, p)
		}
		st, _ := os.Stat(p)
		mine[p] = st
	}
	if len(raw) > 0 {
		return fmt.Errorf("snapshots need qcow2 disks; %s is raw: convert it to qcow2 in the Media manager first", strings.Join(raw, ", "))
	}
	vms, err := ListVMs()
	if err != nil {
		return err
	}
	same := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && slices.ContainsFunc(mapValues(mine), func(m os.FileInfo) bool { return m != nil && os.SameFile(m, st) })
	}
	for _, o := range vms {
		if o.ID == e.ID {
			continue
		}
		for _, st := range diskFiles(o.Config) {
			if slices.ContainsFunc(mapValues(mine), func(m os.FileInfo) bool { return m != nil && os.SameFile(m, st) }) {
				return fmt.Errorf("a disk of this VM is also used by %s: snapshots need disks of its own", o.DisplayName())
			}
		}
		snaps, _ := listSnapshots(o.ID, true)
		odir, _ := o.Config.WorkspaceDir()
		for _, s := range snaps {
			for _, d := range s.Manifest.Disks {
				if same(diskAbs(odir, d)) {
					return fmt.Errorf("a disk of this VM is in a snapshot of %s: snapshots need disks of its own", o.DisplayName())
				}
			}
		}
	}
	return nil
}

func mapValues[K comparable, V any](m map[K]V) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// ListSnapshots is e's snapshot tree in creation order (parents before children).
func ListSnapshots(e VMEntry) ([]Snapshot, error) {
	recoverSnapshotOps(e)
	return listSnapshots(e.ID, false)
}

func listSnapshots(vmID string, withDeleting bool) ([]Snapshot, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, parent_id, name, description, created_at, state, manifest FROM snapshots WHERE vm_id = ? ORDER BY created_at, rowid`, vmID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var s Snapshot
		var manifest string
		if err := rows.Scan(&s.ID, &s.ParentID, &s.Name, &s.Description, &s.CreatedAt, &s.state, &manifest); err != nil {
			return nil, err
		}
		if s.state == "deleting" && !withDeleting {
			continue
		}
		if err := json.Unmarshal([]byte(manifest), &s.Manifest); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", s.ID, err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// journal is an operation's record on disk, written before its first change and after each step.
type journal struct {
	Op    string   `json:"op"`
	Kind  string   `json:"kind"` // take, restore or delete
	VMID  string   `json:"vm_id"`
	Snap  string   `json:"snap"`
	Disks []string `json:"disks,omitempty"` // take: tagged; restore: given the pre-restore tag
	Files bool     `json:"files,omitempty"` // restore: the current state files were saved
	path  string
}

func (j *journal) save() error {
	data, _ := json.Marshal(j)
	f, err := os.OpenFile(j.path+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if err := os.Rename(j.path+".tmp", j.path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(j.path))
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func newJournal(dir, kind, vmID, snap string) (*journal, error) {
	if err := os.MkdirAll(filepath.Join(dir, snapDir), 0o700); err != nil {
		return nil, err
	}
	op := newID()
	j := &journal{Op: op, Kind: kind, VMID: vmID, Snap: snap, path: filepath.Join(dir, snapDir, ".op-"+op+".json")}
	return j, j.save()
}

func (j *journal) remove() {
	os.Remove(j.path)
	syncDir(filepath.Dir(j.path))
}

func qemuImg(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "qemu-img", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img %s: %v: %s", args[0]+" "+args[1], err, strings.TrimSpace(string(out)))
	}
	return nil
}

// hasTag reports whether a qcow2 disk holds an internal snapshot named tag.
func hasTag(ctx context.Context, disk, tag string) (bool, error) {
	out, err := exec.CommandContext(ctx, "qemu-img", "info", "--output=json", disk).Output()
	if err != nil {
		return false, fmt.Errorf("qemu-img info %s: %w", disk, err)
	}
	var info struct {
		Snapshots []struct {
			Name string `json:"name"`
		} `json:"snapshots"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return false, err
	}
	return slices.ContainsFunc(info.Snapshots, func(s struct {
		Name string `json:"name"`
	}) bool {
		return s.Name == tag
	}), nil
}

// dropTag deletes tag from disk; a missing tag or disk is already done.
func dropTag(ctx context.Context, disk, tag string) error {
	if !exists(disk) {
		return nil
	}
	ok, err := hasTag(ctx, disk, tag)
	if err != nil || !ok {
		return err
	}
	return qemuImg(ctx, "snapshot", "-d", tag, disk)
}

// copyState copies the state files that exist from one folder to another (rel paths).
func copyState(from, to string, rels []string) ([]string, error) {
	var copied []string
	for _, rel := range rels {
		src := filepath.Join(from, rel)
		st, err := os.Stat(src)
		if err != nil {
			continue
		}
		dst := filepath.Join(to, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return copied, err
		}
		if st.IsDir() {
			os.RemoveAll(dst)
			if err := copyTree(src, dst); err != nil {
				return copied, err
			}
			filepath.WalkDir(dst, func(p string, d os.DirEntry, _ error) error {
				if d != nil && d.IsDir() {
					os.Chmod(p, 0o700)
				} else {
					os.Chmod(p, 0o600)
				}
				return nil
			})
		} else if err := copyFileAtomic(src, dst, 0o600); err != nil {
			return copied, err
		}
		copied = append(copied, rel)
	}
	return copied, nil
}

// replaceState makes the VM folder's state files those of from (rels it holds), removing the
// others in rels.
func replaceState(dir, from string, rels, have []string) error {
	for _, rel := range rels {
		dst := filepath.Join(dir, rel)
		if !slices.Contains(have, rel) {
			if err := os.RemoveAll(dst); err != nil {
				return err
			}
			continue
		}
		if _, err := copyState(from, dir, []string{rel}); err != nil {
			return err
		}
	}
	return nil
}

// lockForSnapshot takes the VM's locks and refuses a running VM.
func lockForSnapshot(e VMEntry) (string, func(), error) {
	dir, err := e.Config.WorkspaceDir()
	if err != nil {
		return "", nil, err
	}
	if _, ok := FindRun(e); ok {
		return "", nil, errors.New("power the VM off first: snapshots are taken, restored and deleted with the VM powered off")
	}
	unlock, err := (&VM{ID: e.ID}).lockRun(NewWorkspace(dir), e.Config)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			return "", nil, errors.New("the VM is starting or busy: power it off and try again")
		}
		return "", nil, fmt.Errorf("power the VM off first: %w", err)
	}
	return dir, unlock, nil
}

// reload is e's stored entry, for operations that must not act on a stale copy.
func reload(e VMEntry) (VMEntry, error) {
	vms, err := ListVMs()
	if err != nil {
		return e, err
	}
	cur, ok := FindVM(vms, e.ID)
	if !ok {
		return e, fmt.Errorf("VM %s no longer exists", e.ID)
	}
	return cur, nil
}

func step(j *journal, name string) error {
	if err := snapStep(j.Kind, name); err != nil {
		return err
	}
	return nil
}

// TakeSnapshot takes snapshot name of a powered-off VM, under its current snapshot.
func TakeSnapshot(ctx context.Context, e VMEntry, name, description string) (Snapshot, error) {
	if strings.TrimSpace(name) == "" {
		return Snapshot{}, &FieldError{"name", "give the snapshot a name"}
	}
	recoverSnapshotOps(e)
	e, err := reload(e)
	if err != nil {
		return Snapshot{}, err
	}
	if err := checkSnapshotEligible(e); err != nil {
		return Snapshot{}, err
	}
	dir, unlock, err := lockForSnapshot(e)
	if err != nil {
		return Snapshot{}, err
	}
	defer unlock()
	s := Snapshot{ID: newID(), ParentID: e.Config.CurrentSnapshot, Name: strings.TrimSpace(name), Description: description}
	j, err := newJournal(dir, "take", e.ID, s.ID)
	if err != nil {
		return Snapshot{}, err
	}
	fail := func(err error) (Snapshot, error) {
		if !errors.Is(err, errKilled) {
			rollbackTake(context.WithoutCancel(ctx), dir, j)
		}
		return Snapshot{}, err
	}
	s.Manifest.Config = e.Config
	for _, d := range snapshotDisks(e.Config) {
		if err := step(j, "tag "+d); err != nil {
			return fail(err)
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		j.Disks = append(j.Disks, d)
		if err := j.save(); err != nil {
			return fail(err)
		}
		if err := qemuImg(ctx, "snapshot", "-c", snapTag(s.ID), diskAbs(dir, d)); err != nil {
			return fail(fmt.Errorf("disk %s: %w", d, err))
		}
		s.Manifest.Disks = append(s.Manifest.Disks, d)
	}
	if err := step(j, "files"); err != nil {
		return fail(err)
	}
	files, err := copyState(dir, filepath.Join(dir, snapDir, s.ID), stateFiles(e.Config))
	if err != nil {
		return fail(err)
	}
	s.Manifest.Files = files
	if err := step(j, "commit"); err != nil {
		return fail(err)
	}
	if err := commitTake(e, s, j.Op); err != nil {
		return fail(err)
	}
	if err := step(j, "done"); err != nil {
		return s, err
	}
	j.remove()
	return s, nil
}

func commitTake(e VMEntry, s Snapshot, op string) error {
	m, _ := json.Marshal(s.Manifest)
	return inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO snapshots (id, vm_id, parent_id, name, description, manifest) VALUES (?, ?, ?, ?, ?, ?)`,
			s.ID, e.ID, s.ParentID, s.Name, s.Description, string(m)); err != nil {
			return err
		}
		if err := setCurrentSnapshot(tx, e.ID, s.ID, nil); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO snapshot_ops (id) VALUES (?)`, op)
		return err
	})
}

func inTx(fn func(*sql.Tx) error) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// setCurrentSnapshot stores the VM's current snapshot, and its settings when c is given.
func setCurrentSnapshot(tx *sql.Tx, vmID, snap string, c *Config) error {
	cur, err := scanVM(tx.QueryRow(`SELECT `+vmColumns+` FROM vms WHERE id = ?`, vmID))
	if err != nil {
		return err
	}
	cfg := cur.Config
	if c != nil {
		cfg = *c
		cfg.Name, cfg.Workspace, cfg.Description = cur.Config.Name, cur.Config.Workspace, cur.Config.Description
	}
	cfg.CurrentSnapshot = snap
	return updateVM(tx, vmID, cfg)
}

func opDone(op string) bool {
	db, err := openDB()
	if err != nil {
		return false
	}
	defer db.Close()
	var n int
	db.QueryRow(`SELECT count(*) FROM snapshot_ops WHERE id = ?`, op).Scan(&n)
	return n > 0
}

func rollbackTake(ctx context.Context, dir string, j *journal) error {
	for _, d := range j.Disks {
		if err := dropTag(ctx, diskAbs(dir, d), snapTag(j.Snap)); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(dir, snapDir, j.Snap)); err != nil {
		return err
	}
	j.remove()
	return nil
}

// RestoreSnapshot returns a powered-off VM to snapshot id: disks, UEFI variables, TPM state and
// settings. Disks attached since are detached, never deleted.
func RestoreSnapshot(ctx context.Context, e VMEntry, id string) error {
	recoverSnapshotOps(e)
	e, err := reload(e)
	if err != nil {
		return err
	}
	if err := refuseReimsCtl(e.Config, "Snapshots"); err != nil {
		return err
	}
	s, err := findSnapshot(e.ID, id)
	if err != nil {
		return err
	}
	dir, unlock, err := lockForSnapshot(e)
	if err != nil {
		return err
	}
	defer unlock()
	union := snapshotDisks(e.Config)
	for _, d := range s.Manifest.Disks {
		if !slices.ContainsFunc(union, func(u string) bool { return diskAbs(dir, u) == diskAbs(dir, d) }) {
			union = append(union, d)
		}
	}
	for _, d := range s.Manifest.Disks {
		if ok, err := hasTag(ctx, diskAbs(dir, d), snapTag(s.ID)); err != nil || !ok {
			return fmt.Errorf("disk %s no longer holds this snapshot (%v): it cannot be restored", d, err)
		}
	}
	for _, f := range s.Manifest.Files {
		if !exists(filepath.Join(dir, snapDir, s.ID, f)) {
			return fmt.Errorf("the snapshot's copy of %s is missing: it cannot be restored", f)
		}
	}
	rels := slices.Compact(slices.Sorted(slices.Values(append(stateFiles(e.Config), stateFiles(s.Manifest.Config)...))))
	if err := checkFreeSpace(dir, rels); err != nil {
		return err
	}
	j, err := newJournal(dir, "restore", e.ID, s.ID)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		if !errors.Is(err, errKilled) {
			if rerr := rollbackRestore(context.WithoutCancel(ctx), dir, j, rels); rerr != nil {
				return fmt.Errorf("%w (and undoing it failed: %v; it is retried when the VM is opened again)", err, rerr)
			}
		}
		return err
	}
	for _, d := range union {
		if err := step(j, "pre "+d); err != nil {
			return fail(err)
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		j.Disks = append(j.Disks, d)
		if err := j.save(); err != nil {
			return fail(err)
		}
		if err := qemuImg(ctx, "snapshot", "-c", preTag(j.Op), diskAbs(dir, d)); err != nil {
			return fail(fmt.Errorf("disk %s: %w", d, err))
		}
	}
	if err := step(j, "save files"); err != nil {
		return fail(err)
	}
	j.Files = true
	if err := j.save(); err != nil {
		return fail(err)
	}
	if _, err := copyState(dir, filepath.Join(dir, snapDir, ".pre-"+j.Op), rels); err != nil {
		return fail(err)
	}
	for _, d := range s.Manifest.Disks {
		if err := step(j, "apply "+d); err != nil {
			return fail(err)
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := qemuImg(ctx, "snapshot", "-a", snapTag(s.ID), diskAbs(dir, d)); err != nil {
			return fail(fmt.Errorf("disk %s: %w", d, err))
		}
	}
	if err := step(j, "apply files"); err != nil {
		return fail(err)
	}
	if err := replaceState(dir, filepath.Join(dir, snapDir, s.ID), rels, s.Manifest.Files); err != nil {
		return fail(err)
	}
	if err := step(j, "commit"); err != nil {
		return fail(err)
	}
	c := s.Manifest.Config
	if err := inTx(func(tx *sql.Tx) error {
		if err := setCurrentSnapshot(tx, e.ID, s.ID, &c); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO snapshot_ops (id) VALUES (?)`, j.Op)
		return err
	}); err != nil {
		return fail(err)
	}
	if err := step(j, "cleanup"); err != nil {
		return err
	}
	return finishRestore(context.WithoutCancel(ctx), dir, j)
}

// checkFreeSpace needs room for the copies of the current state files, plus a margin.
func checkFreeSpace(dir string, rels []string) error {
	var need int64 = 64 << 20
	for _, rel := range rels {
		filepath.Walk(filepath.Join(dir, rel), func(_ string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() {
				need += fi.Size()
			}
			return nil
		})
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return err
	}
	if free := int64(st.Bavail) * st.Bsize; free < need {
		return fmt.Errorf("not enough free space in %s for a restore: %d MiB needed, %d MiB free", dir, need>>20, free>>20)
	}
	return nil
}

func finishRestore(ctx context.Context, dir string, j *journal) error {
	for _, d := range j.Disks {
		if err := dropTag(ctx, diskAbs(dir, d), preTag(j.Op)); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(dir, snapDir, ".pre-"+j.Op)); err != nil {
		return err
	}
	j.remove()
	return nil
}

func rollbackRestore(ctx context.Context, dir string, j *journal, rels []string) error {
	for _, d := range j.Disks {
		p := diskAbs(dir, d)
		if ok, err := hasTag(ctx, p, preTag(j.Op)); err != nil {
			return err
		} else if ok {
			if err := qemuImg(ctx, "snapshot", "-a", preTag(j.Op), p); err != nil {
				return err
			}
		}
	}
	pre := filepath.Join(dir, snapDir, ".pre-"+j.Op)
	if j.Files && exists(pre) {
		var have []string
		for _, rel := range rels {
			if exists(filepath.Join(pre, rel)) {
				have = append(have, rel)
			}
		}
		if err := replaceState(dir, pre, rels, have); err != nil {
			return err
		}
	}
	return finishRestore(ctx, dir, j)
}

func findSnapshot(vmID, id string) (Snapshot, error) {
	snaps, err := listSnapshots(vmID, false)
	if err != nil {
		return Snapshot{}, err
	}
	for _, s := range snaps {
		if s.ID == id || s.Name == id {
			return s, nil
		}
	}
	return Snapshot{}, fmt.Errorf("this VM has no snapshot %q", id)
}

// DeleteSnapshot deletes snapshot id of a powered-off VM; its children move under its parent.
// Once it is marked deleting it is always finished, here or when the VM is opened again.
func DeleteSnapshot(ctx context.Context, e VMEntry, id string) error {
	recoverSnapshotOps(e)
	e, err := reload(e)
	if err != nil {
		return err
	}
	if err := refuseReimsCtl(e.Config, "Snapshots"); err != nil {
		return err
	}
	s, err := findSnapshot(e.ID, id)
	if err != nil {
		return err
	}
	dir, unlock, err := lockForSnapshot(e)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := newJournal(dir, "delete", e.ID, s.ID)
	if err != nil {
		return err
	}
	if err := step(j, "mark"); err != nil {
		if !errors.Is(err, errKilled) {
			j.remove()
		}
		return err
	}
	if err := inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE snapshots SET state = 'deleting' WHERE id = ?`, s.ID)
		return err
	}); err != nil {
		j.remove()
		return err
	}
	return finishDelete(ctx, dir, j, s)
}

func finishDelete(ctx context.Context, dir string, j *journal, s Snapshot) error {
	for _, d := range s.Manifest.Disks {
		if err := step(j, "untag "+d); err != nil {
			return err
		}
		if err := dropTag(ctx, diskAbs(dir, d), snapTag(s.ID)); err != nil {
			return fmt.Errorf("disk %s: %w (the delete is finished when the VM is opened again)", d, err)
		}
	}
	if err := step(j, "files"); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(dir, snapDir, s.ID)); err != nil {
		return err
	}
	if err := step(j, "commit"); err != nil {
		return err
	}
	if err := inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE snapshots SET parent_id = ? WHERE parent_id = ? AND vm_id = ?`, s.ParentID, s.ID, j.VMID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM snapshots WHERE id = ?`, s.ID); err != nil {
			return err
		}
		cur, err := scanVM(tx.QueryRow(`SELECT `+vmColumns+` FROM vms WHERE id = ?`, j.VMID))
		if err == nil && cur.Config.CurrentSnapshot == s.ID {
			if err := setCurrentSnapshot(tx, j.VMID, s.ParentID, nil); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`INSERT OR IGNORE INTO snapshot_ops (id) VALUES (?)`, j.Op)
		return err
	}); err != nil {
		return err
	}
	j.remove()
	return nil
}

// recoverSnapshotOps finishes or undoes e's interrupted snapshot operations, and finishes deletes
// left marked deleting, before anything else uses the VM. A running VM is left alone.
func recoverSnapshotOps(e VMEntry) error {
	dir, err := e.Config.WorkspaceDir()
	if err != nil || e.ID == "" {
		return err
	}
	journals, _ := filepath.Glob(filepath.Join(dir, snapDir, ".op-*.json"))
	snaps, _ := listSnapshots(e.ID, true)
	deleting := slices.ContainsFunc(snaps, func(s Snapshot) bool { return s.state == "deleting" })
	if len(journals) == 0 && !deleting {
		return nil
	}
	if _, ok := FindRun(e); ok {
		return nil
	}
	unlock, err := (&VM{ID: e.ID}).lockRun(NewWorkspace(dir), e.Config)
	if err != nil {
		return err
	}
	defer unlock()
	ctx := context.Background()
	var errs []error
	deletes := map[string]*journal{}
	for _, p := range journals {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		j := &journal{path: p}
		if json.Unmarshal(data, j) != nil || j.VMID != e.ID {
			continue
		}
		switch j.Kind {
		case "take":
			if opDone(j.Op) {
				j.remove()
			} else {
				errs = append(errs, rollbackTake(ctx, dir, j))
			}
		case "restore":
			s, _ := findSnapshotAny(e.ID, j.Snap)
			rels := slices.Compact(slices.Sorted(slices.Values(append(stateFiles(e.Config), stateFiles(s.Manifest.Config)...))))
			if opDone(j.Op) {
				errs = append(errs, finishRestore(ctx, dir, j))
			} else {
				errs = append(errs, rollbackRestore(ctx, dir, j, rels))
			}
		case "delete":
			deletes[j.Snap] = j // finished below when its mark committed, else nothing was changed
		}
	}
	for _, s := range snaps {
		j := deletes[s.ID]
		delete(deletes, s.ID)
		if s.state != "deleting" {
			if j != nil {
				j.remove()
			}
			continue
		}
		if j == nil {
			j = &journal{Op: newID(), Kind: "delete", VMID: e.ID, Snap: s.ID, path: filepath.Join(dir, snapDir, ".op-recover.json")}
		}
		errs = append(errs, finishDelete(ctx, dir, j, s))
	}
	for _, j := range deletes { // its snapshot is gone: the delete committed
		j.remove()
	}
	return errors.Join(errs...)
}

func findSnapshotAny(vmID, id string) (Snapshot, error) {
	snaps, err := listSnapshots(vmID, true)
	if err != nil {
		return Snapshot{}, err
	}
	for _, s := range snaps {
		if s.ID == id {
			return s, nil
		}
	}
	return Snapshot{}, fmt.Errorf("no snapshot %s", id)
}

// RecoverSnapshots runs recoverSnapshotOps for every VM; the GUI, the CLI and the MCP server
// call it when they open.
func RecoverSnapshots() error {
	vms, err := ListVMs()
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range vms {
		if err := recoverSnapshotOps(e); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.DisplayName(), err))
		}
	}
	return errors.Join(errs...)
}
