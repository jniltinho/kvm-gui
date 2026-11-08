package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jniltinho/kvm-gui/internal/core"
)

// fakeReimsCtl writes a reims-ctl with one VM, "app", forwarding SSH from 2350, and points
// reims_qemu at a QEMU next to it; it logs every call to the returned file.
func fakeReimsCtl(t *testing.T) (calls string) {
	t.Helper()
	root := t.TempDir()
	qemu := filepath.Join(root, "qemu-system-x86_64")
	os.WriteFile(qemu, nil, 0o755)
	calls = filepath.Join(root, "calls")
	os.MkdirAll(filepath.Join(root, "app"), 0o755)
	os.WriteFile(filepath.Join(root, "app.sh"), []byte(fmt.Sprintf("#!/bin/sh\nexec %s -m 8G -smp 4 -netdev 'user,id=n,hostfwd=tcp:127.0.0.1:2350-:22' -drive 'id=disk,if=none,format=qcow2,file=%s/app/disk.qcow2'\n", qemu, root)), 0o644)
	os.WriteFile(filepath.Join(root, "reims-ctl"), []byte(`#!/bin/sh
echo "$*" >> `+calls+`
case "$1" in
list) echo '[{"name":"app","dir":"`+root+`/app","pid":null,"state":"stopped"}]';;
command) cp `+root+`/app.sh "$4";;
create|set) echo "app: done";;
*) exit 2;;
esac
`), 0o755)
	t.Setenv("MKG_REIMS_QEMU", qemu)
	return calls
}

// vm add lists reims-ctl's VMs, then adds one with its configuration folder (task 4.7).
func TestVMAddReimsCtl(t *testing.T) {
	isolateXDG(t, t.TempDir())
	fakeReimsCtl(t)
	t.Cleanup(func() { addReimsCtl, addReimsCtlConfig = "", "" })
	out, err := run(t, "vm", "add")
	if err != nil || !strings.Contains(out, "app\tstopped") {
		t.Fatalf("list: %q %v", out, err)
	}
	out, err = run(t, "vm", "add", "--reims-ctl", "app")
	if err != nil || !strings.Contains(out, "added app (app) from reims-ctl") || !strings.Contains(out, "SSH port 2350") {
		t.Fatalf("add: %q %v", out, err)
	}
	vms, _ := core.ListVMs()
	if len(vms) != 1 || vms[0].Config.Backend != core.BackendReimsCtl || vms[0].Config.ReimsCtlConfig != filepath.Join(core.ConfigDir(), "reims-ctl") {
		t.Fatalf("stored: %+v", vms)
	}
	addReimsCtl = ""
	// The fake lists "app" in the user's own reims-ctl too: only kvm-gui's one is gone.
	if out, _ := run(t, "vm", "add"); strings.Contains(out, filepath.Join(core.ConfigDir(), "reims-ctl")) || !strings.Contains(out, core.UserReimsCtlConfig()) {
		t.Fatalf("after adding: %q", out)
	}
	// A reims-ctl VM refuses move the same way as in core.
	if _, err := run(t, "vm", "move", "app", t.TempDir()); err == nil || !strings.Contains(err.Error(), core.ErrReimsCtlUnsupported.Error()) {
		t.Fatalf("move: %v", err)
	}
}
