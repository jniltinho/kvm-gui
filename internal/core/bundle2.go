package core

import (
	"archive/tar"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Bundle format 2 holds what format 1 cannot: every disk of a Linux, Windows or Other VM under
// disks/, its UEFI variables, its TPM state under tpm/, and its snapshots (rows in the manifest,
// state copies under snapshots/<id>/; the disks carry their internal snapshots). A bundle with TPM
// state is written 0600, and import makes every file 0600 and folder 0700 (design D11).

type bundleSnap struct {
	ID          string       `json:"id"`
	ParentID    string       `json:"parent_id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	CreatedAt   string       `json:"created_at"`
	Manifest    snapManifest `json:"manifest"`
}

var (
	v2Disk    = regexp.MustCompile(`^disks/(disk[0-9]{1,3}\.qcow2)$`)
	v2TPM     = regexp.MustCompile(`^tpm/[A-Za-z0-9._-]{1,64}$`)
	v2Snap    = regexp.MustCompile(`^snapshots/([0-9a-f]{12})/(` + regexp.QuoteMeta(GuestVarsFile) + `|` + regexp.QuoteMeta(GuestVarsKind) + `|` + regexp.QuoteMeta(VarsFile) + `|tpm/[A-Za-z0-9._-]{1,64})$`)
	v2Flat    = map[string]bool{GuestVarsFile: true, GuestVarsKind: true}
	errNoDisk = errors.New("the bundle has no disk")
)

type bundleEntry struct {
	name, path string
	mode       int64
}

// exportV2 writes e in format 2 (the VM's locks are held).
func exportV2(ctx context.Context, e VMEntry, dir string, snaps []Snapshot, dst string, progress func(done, total int64)) error {
	c := e.Config
	var entries []bundleEntry
	newName := map[string]string{} // a disk as configs name it -> its name in the bundle
	addDisk := func(d string) {
		if _, ok := newName[d]; ok {
			return
		}
		n := fmt.Sprintf("disk%d.qcow2", len(newName))
		newName[d] = n
		entries = append(entries, bundleEntry{"disks/" + n, diskAbs(dir, d), 0o600})
	}
	if c.MacOS() {
		for _, f := range bundleFiles {
			if f != DiskFile && exists(filepath.Join(dir, f)) {
				entries = append(entries, bundleEntry{f, filepath.Join(dir, f), 0o600})
			}
		}
	}
	for _, d := range snapshotDisks(c) {
		addDisk(d)
	}
	for _, s := range snaps {
		for _, d := range s.Manifest.Disks {
			addDisk(d) // a disk detached since the snapshot comes along too
		}
	}
	if len(newName) == 0 {
		return fmt.Errorf("%s has no disk: nothing to export", e.DisplayName())
	}
	for f := range v2Flat {
		if exists(filepath.Join(dir, f)) {
			entries = append(entries, bundleEntry{f, filepath.Join(dir, f), 0o600})
		}
	}
	walk := func(root, prefix string) error {
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			entries = append(entries, bundleEntry{prefix + filepath.ToSlash(rel), p, 0o600})
			return nil
		})
	}
	if exists(filepath.Join(dir, GuestTPMDir)) {
		if err := walk(filepath.Join(dir, GuestTPMDir), "tpm/"); err != nil {
			return err
		}
	}
	m := manifest{Format: 2, App: "kvm-gui", Config: c}
	rename := func(c Config) Config {
		c.Disks = append([]Disk(nil), c.Disks...)
		for i, d := range c.Disks {
			c.Disks[i].Path = newName[d.Path]
		}
		return c
	}
	m.Config = rename(c)
	for _, s := range snaps {
		sm := s.Manifest
		sm.Tag, sm.Config = s.tag(), rename(sm.Config)
		sm.Disks = nil
		for _, d := range s.Manifest.Disks {
			sm.Disks = append(sm.Disks, newName[d])
		}
		if c.MacOS() {
			sm.Disks = []string{DiskFile}
		}
		m.Snapshots = append(m.Snapshots, bundleSnap{s.ID, s.ParentID, s.Name, s.Description, s.CreatedAt, sm})
		if err := walk(filepath.Join(dir, snapDir, s.ID), "snapshots/"+s.ID+"/"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if c.MacOS() { // the macOS disk keeps its own name, as format 1
		for i := range entries {
			if strings.HasPrefix(entries[i].name, "disks/") {
				entries[i].name = "disks/disk0.qcow2"
			}
		}
	}
	perm := os.FileMode(0o644)
	var total int64
	for _, en := range entries {
		if strings.HasPrefix(en.name, "tpm/") || strings.Contains(en.name, "/tpm/") {
			perm = 0o600
		}
		if st, err := os.Stat(en.path); err == nil {
			total += st.Size()
		}
	}
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			out.Close()
			os.Remove(tmp)
		}
	}()
	tw := tar.NewWriter(out)
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := tw.WriteHeader(&tar.Header{Name: bundleManifest, Mode: 0o600, Size: int64(len(mb))}); err != nil {
		return err
	}
	if _, err := tw.Write(mb); err != nil {
		return err
	}
	var done int64
	for _, en := range entries {
		if err := addFileMode(ctx, tw, en.path, en.name, en.mode, &done, total, progress); err != nil {
			return fmt.Errorf("exporting %s: %w", en.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ok = true
	return os.Rename(tmp, dst)
}

func addFileMode(ctx context.Context, tw *tar.Writer, path, name string, mode int64, done *int64, total int64, progress func(int64, int64)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, &progressReader{ctx: ctx, r: f, done: done, total: total, report: progress})
	return err
}

// importV2 unpacks a format 2 bundle into workspace and adds the VM with its snapshots under new
// ids. Only the allowlisted names are accepted; files are made 0600 and folders 0700.
func importV2(ctx context.Context, tr *tar.Reader, m manifest, workspace string, size int64, progress func(done, total int64)) (VMEntry, error) {
	created := !exists(workspace)
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return VMEntry{}, err
	}
	os.Chmod(workspace, 0o700)
	var written []string
	cleanup := func() {
		for i := len(written) - 1; i >= 0; i-- {
			os.RemoveAll(written[i])
		}
		os.RemoveAll(filepath.Join(workspace, GuestTPMDir))
		os.RemoveAll(filepath.Join(workspace, snapDir))
		os.RemoveAll(filepath.Join(workspace, BootDir))
		if created {
			os.Remove(workspace)
		}
	}
	newID := map[string]string{"": ""}
	for _, s := range m.Snapshots {
		if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(s.ID) {
			return VMEntry{}, fmt.Errorf("refusing bundle snapshot id %q", s.ID)
		}
		newID[s.ID] = newIDFor()
	}
	var disks []string
	var done int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return VMEntry{}, err
		}
		if h.Typeflag != tar.TypeReg {
			cleanup()
			return VMEntry{}, fmt.Errorf("refusing unexpected bundle entry %q", h.Name)
		}
		var target string
		switch {
		case v2Disk.MatchString(h.Name):
			target = filepath.Join(workspace, v2Disk.FindStringSubmatch(h.Name)[1])
			if m.Config.MacOS() {
				target = filepath.Join(workspace, DiskFile)
			}
			disks = append(disks, target)
		case v2Flat[h.Name], m.Config.MacOS() && isBundleFile(h.Name) && h.Name != DiskFile:
			target = filepath.Join(workspace, h.Name)
		case v2TPM.MatchString(h.Name):
			target = filepath.Join(workspace, filepath.FromSlash(h.Name))
		case v2Snap.MatchString(h.Name):
			sm := v2Snap.FindStringSubmatch(h.Name)
			id, ok := newID[sm[1]]
			if !ok {
				cleanup()
				return VMEntry{}, fmt.Errorf("refusing bundle entry %q: no such snapshot in the manifest", h.Name)
			}
			target = filepath.Join(workspace, snapDir, id, filepath.FromSlash(sm[2]))
		default:
			cleanup()
			return VMEntry{}, fmt.Errorf("refusing unexpected bundle entry %q", h.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			cleanup()
			return VMEntry{}, err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			cleanup()
			return VMEntry{}, err
		}
		written = append(written, target)
		_, err = io.Copy(out, &progressReader{ctx: ctx, r: io.LimitReader(tr, h.Size), done: &done, total: size, report: progress})
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			cleanup()
			return VMEntry{}, err
		}
	}
	if len(disks) == 0 {
		cleanup()
		return VMEntry{}, errNoDisk
	}
	for _, d := range disks {
		if err := checkStandaloneDisk(d); err != nil {
			cleanup()
			return VMEntry{}, err
		}
	}
	c := m.Config
	c.Workspace, c.GPU, c.CurrentSnapshot = workspace, "", newID[c.CurrentSnapshot]
	for _, d := range c.Disks {
		if filepath.IsAbs(d.Path) || !filepath.IsLocal(d.Path) {
			cleanup()
			return VMEntry{}, fmt.Errorf("refusing bundle disk path %q", d.Path)
		}
	}
	e, err := createImported(c)
	if err != nil {
		cleanup()
		return VMEntry{}, err
	}
	err = inTx(func(tx *sql.Tx) error {
		for _, s := range m.Snapshots {
			sm := s.Manifest
			if sm.Tag == "" {
				sm.Tag = snapTag(s.ID)
			}
			for _, d := range sm.Disks {
				if filepath.IsAbs(d) || !filepath.IsLocal(d) {
					return fmt.Errorf("refusing snapshot disk path %q", d)
				}
			}
			mb, _ := json.Marshal(sm)
			if _, err := tx.Exec(`INSERT INTO snapshots (id, vm_id, parent_id, name, description, created_at, manifest) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				newID[s.ID], e.ID, newID[s.ParentID], s.Name, s.Description, s.CreatedAt, string(mb)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		DeleteVM(e, false)
		cleanup()
		return VMEntry{}, err
	}
	return e, nil
}

func isBundleFile(name string) bool {
	for _, f := range bundleFiles {
		if f == name {
			return true
		}
	}
	return false
}

func newIDFor() string { return newID() }
