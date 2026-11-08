package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The starter, the GUI and an agent are connected at once, each on its own monitor: the agent's
// commands work while the GUI keeps receiving events, and the GUI reconnects after a drop (task 4.2).
func TestThreeMonitors(t *testing.T) {
	qemu, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		needTool(t, "qemu-system-x86_64 missing")
	}
	dir, err := os.MkdirTemp(runBase(), "qmp-") // short: unix socket paths are limited to 108 bytes
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "qmp.sock")
	cmd := exec.Command(qemu, append([]string{"-machine", "none", "-nodefaults", "-display", "none"}, qmpArgs(sock)...)...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	owner, err := DialQMP(sock, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	events := make(chan string, 16)
	conns := make(chan *QMP, 4)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	kept := make(chan error, 1)
	go func() {
		kept <- KeepQMP(ctx, GUISocket(sock), func(e QMPEvent) { events <- e.Name }, func(q *QMP) { conns <- q })
	}()
	gui := <-conns
	agent, err := DialQMP(ControlSocket(sock), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	wait := func(want string) {
		t.Helper()
		select {
		case e := <-events:
			if e != want {
				t.Fatalf("event %s, want %s", e, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s event on the GUI monitor", want)
		}
	}
	if err := agent.Execute("stop"); err != nil {
		t.Fatal(err)
	}
	wait("STOP")
	if _, err := agent.Query("query-status"); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Query("query-status"); err != nil {
		t.Fatal(err)
	}
	// The GUI's connection drops: it comes back on its own and gets the next event.
	gui.Close()
	select {
	case <-conns:
	case <-time.After(10 * time.Second):
		t.Fatal("the GUI client did not reconnect")
	}
	if err := agent.Execute("cont"); err != nil {
		t.Fatal(err)
	}
	wait("RESUME")
	cancel()
	if err := <-kept; err != context.Canceled {
		t.Fatalf("KeepQMP: %v", err)
	}
}
