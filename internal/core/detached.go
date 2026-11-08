package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// StartOptions are the hidden flags of a detached `vm start`.
type StartOptions struct {
	Starter       string // StarterGUI or StarterMCP
	EmbedX11      bool   // GUI runs only: Reims vGPU opens its window on X11, to be embedded
	VerboseBoot   bool
	KextsSnapshot string // the extra kexts the caller downloaded (JSON)
}

// startWait is how long StartDetached waits for the run's record.
var startWait = 30 * time.Second

// StartDetached runs `<self> vm start` for e in its own session, output to the start log in the
// state folder, so the run outlives the caller: the GUI or the MCP server may exit and the VM goes
// on (design D2). It returns once the run's record names e.
func StartDetached(ctx context.Context, self string, e VMEntry, o StartOptions) (RunInfo, error) {
	if o.Starter != StarterGUI && o.Starter != StarterMCP {
		return RunInfo{}, fmt.Errorf("a detached start is the GUI's or the MCP server's, not %q", o.Starter)
	}
	if o.EmbedX11 && o.Starter != StarterGUI {
		return RunInfo{}, errors.New("only a GUI run is embedded")
	}
	logDir := filepath.Join(StateDir(), "logs", e.ID)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return RunInfo{}, err
	}
	logPath := filepath.Join(logDir, o.Starter+"-start.log")
	log, err := os.Create(logPath)
	if err != nil {
		return RunInfo{}, err
	}
	defer log.Close()
	args := []string{"vm", "start", "--vm", e.ID, "--display", DisplayApp}
	if o.Starter == StarterGUI {
		args = append(args, "--gui-run")
	} else {
		args = append(args, "--starter", o.Starter)
	}
	if o.EmbedX11 {
		args = append(args, "--embed-x11")
	}
	if o.KextsSnapshot != "" {
		args = append(args, "--kexts-snapshot", o.KextsSnapshot)
	}
	if o.VerboseBoot {
		args = append(args, "--verbose-boot")
	}
	cmd := exec.Command(self, args...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // no Pdeathsig: the run outlives its caller
	if err := cmd.Start(); err != nil {
		return RunInfo{}, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	for deadline := time.Now().Add(startWait); time.Now().Before(deadline); {
		if r, ok := FindRun(e); ok && r.VMID == e.ID {
			return r, nil
		}
		select {
		case <-exited:
			b, _ := os.ReadFile(logPath)
			return RunInfo{}, fmt.Errorf("the VM did not start: %s", strings.TrimSpace(string(b)))
		case <-ctx.Done():
			return RunInfo{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return RunInfo{}, fmt.Errorf("the VM did not show up within %s; see %s", startWait, logPath)
}

// ClaimGUIRun gives this GUI e's run to embed: only a GUI run whose record still matches its
// process (pid and start time), and only while no other GUI holds its gui-run.lock. release frees
// the lock; it is also freed when this process exits.
func ClaimGUIRun(e VMEntry) (r RunInfo, release func(), err error) {
	r, ok := FindRun(e)
	if !ok || r.VMID != e.ID {
		return RunInfo{}, nil, fmt.Errorf("%s has no run of this app", e.DisplayName())
	}
	if r.Starter != StarterGUI {
		return RunInfo{}, nil, fmt.Errorf("%s was started by the %s: only GUI runs are embedded", e.DisplayName(), strings.ToUpper(r.Starter))
	}
	f, err := os.OpenFile(filepath.Join(RunDir(e.ID), "gui-run.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return RunInfo{}, nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return RunInfo{}, nil, fmt.Errorf("%s's screen is open in another window of the app: %w", e.DisplayName(), ErrBusy)
		}
		return RunInfo{}, nil, err
	}
	return r, func() { f.Close() }, nil
}
