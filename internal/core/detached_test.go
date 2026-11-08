package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeVMStart is the test binary run as a detached `kvm-gui vm start`: it starts a fake QEMU
// (a copy of sh named qemu-system-x86_64, FAKE_QEMU) in the VM's run directory and records it.
func fakeVMStart() {
	id := os.Args[slices.Index(os.Args, "--vm")+1]
	starter := StarterCLI
	if slices.Contains(os.Args, "--gui-run") {
		starter = StarterGUI
	} else if i := slices.Index(os.Args, "--starter"); i > 0 {
		starter = os.Args[i+1]
	}
	dir, err := makeRunDir(id)
	if err != nil {
		os.Exit(2)
	}
	q := exec.Command(os.Getenv("FAKE_QEMU"), "-c", "sleep 60", "qemu", "-qmp", "unix:"+filepath.Join(dir, "qmp.sock")+",server=on,wait=off")
	if q.Start() != nil || writeRunRecord(dir, id, starter, q.Process.Pid) != nil {
		os.Exit(2)
	}
	q.Wait()
	os.Exit(0)
}

// fakeGUI is the test binary run as the GUI: it starts the VMs in FAKE_GUI_VMS detached, then exits.
func fakeGUI() {
	for _, id := range strings.Split(os.Getenv("FAKE_GUI_VMS"), ",") {
		if _, err := StartDetached(context.Background(), os.Args[0], disklessVM(id), StartOptions{Starter: StarterGUI}); err != nil {
			os.Stderr.WriteString(err.Error())
			os.Exit(1)
		}
	}
	os.Exit(0)
}

// disklessVM is a VM found only through its run record (its disk does not exist).
func disklessVM(id string) VMEntry {
	return VMEntry{ID: id, Config: Config{Workspace: "/nonexistent-" + id}}
}

// Two VMs started by the GUI survive it; only a GUI run whose record matches its process is
// embedded, by one GUI at a time; CLI and MCP runs never are (task 4.3).
func TestGUIRunsSurviveTheGUI(t *testing.T) {
	sh, err := os.ReadFile("/bin/sh")
	if err != nil {
		t.Skip(err)
	}
	fake := filepath.Join(t.TempDir(), "qemu-system-x86_64")
	os.WriteFile(fake, sh, 0o755)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("FAKE_QEMU", fake)
	gui := exec.Command(os.Args[0])
	gui.Env = append(os.Environ(), "FAKE_GUI=1", "FAKE_GUI_VMS=one,two")
	if out, err := gui.CombinedOutput(); err != nil {
		t.Fatalf("the GUI: %v %s", err, out)
	}
	for _, id := range []string{"one", "two"} {
		e := disklessVM(id)
		r, release, err := ClaimGUIRun(e)
		if err != nil {
			t.Fatalf("%s after the GUI exited: %v", id, err)
		}
		t.Cleanup(func() { syscall.Kill(r.PID, syscall.SIGKILL) })
		if _, _, err := ClaimGUIRun(e); !errors.Is(err, ErrBusy) {
			t.Fatalf("a second GUI embeds %s: %v", id, err)
		}
		release()
		_, release, err = ClaimGUIRun(e)
		if err != nil {
			t.Fatalf("after release: %v", err)
		}
		release()
	}
	// A reused pid: the record's start time no longer matches, so it is no GUI run.
	data, _ := os.ReadFile(filepath.Join(RunDir("one"), "run.json"))
	os.WriteFile(filepath.Join(RunDir("one"), "run.json"), []byte(strings.Replace(string(data), `"start_ticks":`, `"start_ticks":1`, 1)), 0o600)
	if _, _, err := ClaimGUIRun(disklessVM("one")); err == nil {
		t.Fatal("a record with another start time was embedded")
	}
	// CLI and MCP runs are never GUI runs; an MCP start never embeds.
	for _, starter := range []string{StarterCLI, StarterMCP} {
		id := "by-" + starter
		e := disklessVM(id)
		args := []string{"vm", "start", "--vm", id, "--starter", starter}
		owner := exec.Command(os.Args[0], args...)
		owner.Env = os.Environ()
		if err := owner.Start(); err != nil {
			t.Fatal(err)
		}
		var r RunInfo
		for range 100 {
			var ok bool
			if r, ok = FindRun(e); ok {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Cleanup(func() { syscall.Kill(r.PID, syscall.SIGKILL); owner.Wait() })
		if r.Starter != starter {
			t.Fatalf("%s run: %+v", starter, r)
		}
		if _, _, err := ClaimGUIRun(e); err == nil || !strings.Contains(err.Error(), "only GUI runs") {
			t.Fatalf("a %s run was embedded: %v", starter, err)
		}
	}
	if _, err := StartDetached(t.Context(), os.Args[0], disklessVM("x"), StartOptions{Starter: StarterMCP, EmbedX11: true}); err == nil {
		t.Fatal("an MCP start with --embed-x11")
	}
}

// A detached run starts from the AppImage when the caller runs from one.
func TestSelfPath(t *testing.T) {
	app := filepath.Join(t.TempDir(), "kvm-gui.AppImage")
	os.WriteFile(app, []byte("x"), 0o755)
	t.Setenv("APPIMAGE", app)
	if p, _ := SelfPath(); p != app {
		t.Fatalf("with APPIMAGE: %s", p)
	}
	t.Setenv("APPIMAGE", "relative")
	if p, _ := SelfPath(); p == "relative" {
		t.Fatal("a relative APPIMAGE was used")
	}
}
