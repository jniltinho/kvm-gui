package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CloneOptions are a full clone's choices.
type CloneOptions struct {
	Folder   string // the clone's folder (default <Workspace>/<name>); it must not exist yet
	NewTPM   bool   // a fresh TPM instead of a copy (BitLocker then asks for its recovery key)
	KeepMACs bool   // keep the adapters' MAC addresses (default: new ones)
}

// CloneVM makes a full, independent copy of a powered-off VM: every hard disk, its own or an
// external one, converted into the new folder (sources are only read), the UEFI variables and the
// TPM state, new MACs, the next free SSH port and, for macOS, a new SMBIOS identity. Snapshots
// are not copied. A failure removes the new folder and registers nothing (design D11).
func CloneVM(ctx context.Context, e VMEntry, name string, o CloneOptions, progress func(string)) (VMEntry, error) {
	if strings.TrimSpace(name) == "" {
		return VMEntry{}, &FieldError{"name", "give the clone a name"}
	}
	if progress == nil {
		progress = func(string) {}
	}
	recoverSnapshotOps(e)
	e, err := reload(e)
	if err != nil {
		return VMEntry{}, err
	}
	if err := refuseReimsCtl(e.Config, "Clone"); err != nil {
		return VMEntry{}, err
	}
	src, err := e.Config.WorkspaceDir()
	if err != nil {
		return VMEntry{}, err
	}
	if _, ok := FindRun(e); ok {
		return VMEntry{}, errors.New("power the VM off first: a clone is made from a powered-off VM")
	}
	unlock, err := (&VM{ID: e.ID}).lockRun(NewWorkspace(src), e.Config)
	if err != nil {
		return VMEntry{}, fmt.Errorf("power the VM off first: %w", err)
	}
	defer unlock()

	c := e.Config
	c.Name, c.Workspace, c.SSHPort, c.CurrentSnapshot, c.Description = strings.TrimSpace(name), o.Folder, 0, "", e.Config.Description
	if c.Workspace == "" {
		c.Workspace = VMFolder(c.Name)
	}
	dst, err := c.WorkspaceDir()
	if err != nil {
		return VMEntry{}, err
	}
	if exists(dst) {
		return VMEntry{}, &FieldError{"folder", dst + " already exists: choose another folder"}
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return VMEntry{}, err
	}
	fail := func(err error) (VMEntry, error) {
		os.RemoveAll(dst)
		return VMEntry{}, err
	}
	if !o.KeepMACs {
		c.Networks = append([]NIC(nil), c.Networks...)
		for i := range c.Networks {
			c.Networks[i].MAC = RandomMAC()
		}
	}
	files := stateFiles(e.Config)
	if o.NewTPM {
		files = []string{GuestVarsFile, GuestVarsKind}
	}
	if _, err := copyState(src, dst, files); err != nil {
		return fail(err)
	}
	if c.MacOS() {
		progress("copying " + DiskFile)
		if err := snapStep("clone", "disk "+DiskFile); err != nil {
			return fail(err)
		}
		if err := qemuImg(ctx, "convert", "-O", "qcow2", filepath.Join(src, DiskFile), filepath.Join(dst, DiskFile)); err != nil {
			return fail(fmt.Errorf("disk %s: %w", DiskFile, err))
		}
	}
	c.Disks = append([]Disk(nil), c.Disks...)
	for i, d := range c.Disks {
		from, err := e.Config.DiskPath(d)
		if err != nil {
			return fail(err)
		}
		to := fmt.Sprintf("disk%d.qcow2", i)
		progress("copying " + from)
		if err := snapStep("clone", "disk "+d.Path); err != nil {
			return fail(err)
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := qemuImg(ctx, "convert", "-O", "qcow2", from, filepath.Join(dst, to)); err != nil {
			return fail(fmt.Errorf("disk %s: %w", d.Path, err))
		}
		os.Chmod(filepath.Join(dst, to), 0o600)
		c.Disks[i] = Disk{Path: to, Format: "qcow2", Bus: d.Bus}
	}
	if err := snapStep("clone", "register"); err != nil {
		return fail(err)
	}
	n, err := CreateVM(c)
	if err != nil {
		return fail(err)
	}
	return n, nil
}
