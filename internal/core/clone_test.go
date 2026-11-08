package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A full clone: an external boot disk and an own disk copied into the new folder (the source
// unchanged), VARS and TPM copied, new MACs and port, no snapshots; "New TPM"; a forced copy
// failure leaves no folder and no row (task 5.3).
func TestCloneVM(t *testing.T) {
	needQemuImg(t)
	isolateXDG(t, t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	ctx := t.Context()
	ext := filepath.Join(t.TempDir(), "boot.qcow2")
	e := snapVM(t, "Source", ext, "disk1.qcow2") // creates the external disk too
	fill(t, ext, 7)
	if _, err := TakeSnapshot(ctx, e, "Clean", ""); err != nil {
		t.Fatal(err)
	}
	e, _ = reload(e)
	before := fileSum(t, ext)
	var progress []string
	n, err := CloneVM(ctx, e, "Copy", CloneOptions{}, func(s string) { progress = append(progress, s) })
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := n.Config.WorkspaceDir()
	if n.Config.Disks[0].Path != "disk0.qcow2" || n.Config.Disks[1].Path != "disk1.qcow2" || pattern(t, filepath.Join(dst, "disk0.qcow2")) != 7 {
		t.Fatalf("disks: %+v", n.Config.Disks)
	}
	if fileSum(t, ext) != before {
		t.Fatal("the external source disk changed")
	}
	if n.Config.Networks[0].MAC == e.Config.Networks[0].MAC || n.Config.SSHPort == e.Config.SSHPort || n.Config.CurrentSnapshot != "" {
		t.Fatalf("identity: %+v", n.Config)
	}
	if readFile(t, filepath.Join(dst, GuestVarsFile)) != "vars-1" || readFile(t, filepath.Join(dst, GuestTPMDir, "tpm2-00.permall")) != "tpm-1" || len(progress) != 2 {
		t.Fatal("VARS, TPM or progress")
	}
	if ok, _ := hasTag(ctx, filepath.Join(dst, "disk1.qcow2"), snapTag(e.Config.CurrentSnapshot)); ok {
		t.Fatal("the clone holds the source's snapshot")
	}
	if snaps, _ := ListSnapshots(n); len(snaps) != 0 {
		t.Fatal("snapshots are not cloned")
	}
	// New TPM.
	fresh, err := CloneVM(ctx, e, "Fresh", CloneOptions{NewTPM: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fdir, _ := fresh.Config.WorkspaceDir()
	if exists(filepath.Join(fdir, GuestTPMDir)) || !exists(filepath.Join(fdir, GuestVarsFile)) {
		t.Fatal("New TPM: no TPM state, the variables copied")
	}
	// A failure on the second disk: no folder, no row.
	count := len(mustList(t))
	snapStep = func(kind, s string) error {
		if kind == "clone" && s == "disk disk1.qcow2" {
			return errors.New("injected copy failure")
		}
		return nil
	}
	t.Cleanup(func() { snapStep = func(string, string) error { return nil } })
	folder := filepath.Join(t.TempDir(), "broken")
	if _, err := CloneVM(ctx, e, "Broken", CloneOptions{Folder: folder}, nil); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("forced failure: %v", err)
	}
	snapStep = func(string, string) error { return nil }
	if exists(folder) || len(mustList(t)) != count {
		t.Fatal("a failed clone left a folder or a row")
	}
	if _, err := CloneVM(ctx, e, "Again", CloneOptions{Folder: dst}, nil); err == nil {
		t.Fatal("cloned into an existing folder")
	}
	os.RemoveAll(filepath.Join(t.TempDir(), "unused"))
}
