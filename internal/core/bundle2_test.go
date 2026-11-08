package core

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Windows 11 VM with two disks, a TPM and snapshots goes through format 2 and comes back whole:
// the bundle is 0600 with 0600 TPM entries, every imported file 0600 and folder 0700, and its
// snapshots restore under new ids (task 5.4).
func TestBundleV2RoundTrip(t *testing.T) {
	needQemuImg(t)
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	ctx := t.Context()
	ext := filepath.Join(t.TempDir(), "data.qcow2")
	e := snapVM(t, "Win11", "disk0.qcow2", ext)
	cur, _ := reload(e)
	c := cur.Config
	c.Guest, c.Profile, c.TPM = GuestWindows, "windows11", true
	if err := SaveVM(cur, c); err != nil {
		t.Fatal(err)
	}
	e, _ = reload(e)
	a, err := TakeSnapshot(ctx, e, "Clean", "fresh install")
	if err != nil {
		t.Fatal(err)
	}
	setState(t, e, 5, "vars-5")
	if _, err := TakeSnapshot(ctx, e, "Updated", ""); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "win11.tar")
	if err := ExportBundle(ctx, e, bundle, nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(bundle); st.Mode().Perm() != 0o600 {
		t.Fatalf("bundle mode %v", st.Mode())
	}
	f, _ := os.Open(bundle)
	tr := tar.NewReader(f)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		names = append(names, h.Name)
		if strings.Contains(h.Name, "tpm/") && h.Mode != 0o600 {
			t.Errorf("%s mode %o", h.Name, h.Mode)
		}
	}
	f.Close()
	joined := strings.Join(names, " ")
	for _, want := range []string{"disks/disk0.qcow2", "disks/disk1.qcow2", "tpm/tpm2-00.permall", GuestVarsFile, "snapshots/" + a.ID + "/tpm/tpm2-00.permall"} {
		if !strings.Contains(joined, want) {
			t.Errorf("bundle lacks %s: %s", want, joined)
		}
	}
	ws := filepath.Join(t.TempDir(), "imported")
	n, err := ImportBundle(ctx, bundle, ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n.Config.Guest != GuestWindows || !n.Config.TPM || n.Config.Disks[1].Path != "disk1.qcow2" || pattern(t, filepath.Join(ws, "disk0.qcow2")) != 5 {
		t.Fatalf("imported: %+v", n.Config)
	}
	filepath.Walk(ws, func(p string, fi os.FileInfo, _ error) error {
		if fi.IsDir() && fi.Mode().Perm() != 0o700 || !fi.IsDir() && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", p, fi.Mode())
		}
		return nil
	})
	snaps, _ := ListSnapshots(n)
	if len(snaps) != 2 || snaps[0].ID == a.ID || snaps[1].ParentID != snaps[0].ID || n.Config.CurrentSnapshot != snaps[1].ID {
		t.Fatalf("imported snapshots: %+v current %s", snaps, n.Config.CurrentSnapshot)
	}
	if err := RestoreSnapshot(ctx, n, "Clean"); err != nil {
		t.Fatal(err)
	}
	if pattern(t, filepath.Join(ws, "disk1.qcow2")) != 1 || readFile(t, filepath.Join(ws, GuestVarsFile)) != "vars-1" ||
		readFile(t, filepath.Join(ws, GuestTPMDir, "tpm2-00.permall")) != "tpm-1" {
		t.Fatal("restoring the imported Clean")
	}
	// The source is untouched by the export, and the same bundle imports twice.
	if _, err := ImportBundle(ctx, bundle, filepath.Join(t.TempDir(), "again"), nil); err != nil {
		t.Fatalf("second import: %v", err)
	}
}

// Format 2 accepts only its allowlisted names.
func TestBundleV2RejectsTraversal(t *testing.T) {
	isolateXDG(t, t.TempDir())
	for _, name := range []string{"../../escape.txt", "disks/../../x.qcow2", "tpm/../../x", "snapshots/0123456789ab/../../../x", "snapshots/0123456789ab/tpm/../x", "disks/disk0.raw"} {
		evil := filepath.Join(t.TempDir(), "evil.tar")
		f, _ := os.Create(evil)
		tw := tar.NewWriter(f)
		m := []byte(`{"format":2,"config":{"name":"Evil","guest":"linux","workspace":"/tmp/x"},"snapshots":[{"id":"0123456789ab","manifest":{"disks":[]}}]}`)
		tw.WriteHeader(&tar.Header{Name: bundleManifest, Mode: 0o644, Size: int64(len(m))})
		tw.Write(m)
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 2})
		tw.Write([]byte("hi"))
		tw.Close()
		f.Close()
		parent := t.TempDir()
		ws := filepath.Join(parent, "ws")
		if _, err := ImportBundle(context.Background(), evil, ws, nil); err == nil || !strings.Contains(err.Error(), "unexpected bundle entry") {
			t.Errorf("%s: %v", name, err)
		}
		if exists(filepath.Join(parent, "escape.txt")) || exists(filepath.Join(parent, "x")) || exists(ws) {
			t.Fatalf("%s: something was left behind", name)
		}
	}
	if vms, _ := ListVMs(); len(vms) != 0 {
		t.Fatal("no VM may be added")
	}
}

// Delete keeps an external image and refuses while another VM uses the VM's own disk; move
// refuses too (task 5.4).
func TestOwnedFilesShared(t *testing.T) {
	needQemuImg(t)
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	ext := filepath.Join(t.TempDir(), "shared-data.qcow2")
	a := snapVM(t, "A", "disk0.qcow2", ext)
	adir, _ := a.Config.WorkspaceDir()
	b := testGuest(t, "ubuntu")
	b.Name, b.Workspace, b.Optical = "B", filepath.Join(t.TempDir(), "b"), nil
	b.Disks = []Disk{{Path: filepath.Join(adir, "disk0.qcow2"), Format: "qcow2", Bus: BusVirtio}}
	be, err := CreateVM(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteVM(a, true); err == nil || !strings.Contains(err.Error(), "B uses one of its disks") {
		t.Fatalf("delete while B uses A's disk: %v", err)
	}
	if _, err := MoveVM(a, filepath.Join(t.TempDir(), "moved")); err == nil || !strings.Contains(err.Error(), "B uses") {
		t.Fatalf("move while B uses A's disk: %v", err)
	}
	if !exists(filepath.Join(adir, "disk0.qcow2")) {
		t.Fatal("a refused delete removed files")
	}
	DeleteVM(be, false)
	if _, err := DeleteVM(a, true); err != nil {
		t.Fatal(err)
	}
	if !exists(ext) || exists(filepath.Join(adir, "disk0.qcow2")) {
		t.Fatal("purge removes the VM's own disk and keeps the external image")
	}
}
