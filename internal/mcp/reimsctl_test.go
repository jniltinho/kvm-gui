package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// vm_create with backend reims-ctl runs reims-ctl create and registers the VM (task 4.7).
func TestVMCreateReimsCtl(t *testing.T) {
	cs := session(t)
	calls := fakeReimsCtl(t)
	out := callOut(t, cs, "vm_create", map[string]any{"name": "app", "backend": "reims-ctl", "version": "tahoe", "folder": filepath.Join(t.TempDir(), "app")})
	cfg := out
	if cfg["backend"] != "reims-ctl" || cfg["reims_ctl_name"] != "app" {
		t.Fatalf("vm_create: %v", out)
	}
	b, _ := os.ReadFile(calls)
	if !strings.Contains(string(b), "create --name app --dir ") || !strings.Contains(string(b), "--macos tahoe") {
		t.Fatalf("calls:\n%s", b)
	}
	if msg, isErr := call(t, cs, "vm_create", map[string]any{"name": "x", "backend": "libvirt"}); !isErr || !strings.Contains(msg, "unknown backend") {
		t.Fatalf("unknown backend: %q", msg)
	}
}
