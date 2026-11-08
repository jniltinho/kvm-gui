package embed

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// When the app dies without Release (SIGKILL), the X server's save-set moves the VM's window to
// the closest ancestor the app did not create. Under a reparenting window manager (mutter, KWin)
// that is the frame of the app's window, which the window manager then destroys, and the VM's
// window with it. A guard, a process of its own started for each embedded window, moves the window
// from wherever it lands back to the root first. shortcut: it races the window manager's frame
// teardown, which takes it several round trips (mutter through its frames client, KWin after its
// close effect); a guard that loses leaves the VM without its window, as without a guard.

// guardStop is what Release writes to the guard: the move that follows is the app's own.
const guardStop = "release\n"

// StartGuard starts prog with args, then the child's and the container's window ids, as this
// window's guard (its main calls Guard); Release stops it before giving the window back.
func (e *Embedded) StartGuard(prog string, args ...string) error {
	cmd := exec.Command(prog, append(args, strconv.FormatUint(uint64(e.child), 10), strconv.FormatUint(uint64(e.container), 10))...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // a signal to the app's process group spares it
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// It reports once it watches the window; until then it could not catch a move.
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "watching\n" {
		in.Close()
		cmd.Wait()
		return errors.New("the embed guard did not start")
	}
	e.guard, e.guardIn = cmd, in
	return nil
}

// stopGuard tells the guard the next move is the app's and waits for it to exit.
func (e *Embedded) stopGuard() {
	if e.guard == nil {
		return
	}
	io.WriteString(e.guardIn, guardStop)
	e.guardIn.Close()
	done := make(chan struct{})
	go func() { e.guard.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		e.guard.Process.Kill()
		<-done
	}
	e.guard = nil
}

// Guard is the guard's main: it watches child (inside container) until the app writes guardStop
// on in, the child is destroyed, or the app is gone (in closes without it) and the window is safe.
// A move of the child out of the container that the app did not announce is the save-set's after
// the app died: the child goes back to the root, mapped, a window of its own again.
func Guard(display string, child, container uint32, in io.Reader, ready io.Writer) error {
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		return err
	}
	defer conn.Close()
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	c, ctr := xproto.Window(child), xproto.Window(container)
	if err := xproto.ChangeWindowAttributesChecked(conn, c, xproto.CwEventMask, []uint32{xproto.EventMaskStructureNotify}).Check(); err != nil {
		return err
	}
	if t, err := xproto.QueryTree(conn, c).Reply(); err != nil || t.Parent != ctr {
		return errors.New("the VM's window is not in its container")
	}
	io.WriteString(ready, "watching\n")
	stop := make(chan bool, 1) // true: the app's release; false: the app is gone
	go func() {
		line, _ := bufio.NewReader(in).ReadString('\n')
		stop <- line == guardStop
	}()
	events := make(chan xgb.Event)
	go func() {
		for {
			ev, err := conn.WaitForEvent()
			if ev == nil && err == nil {
				close(events)
				return
			}
			if ev != nil {
				events <- ev
			}
		}
	}()
	var gone <-chan time.Time
	for {
		select {
		case released := <-stop:
			if released {
				return nil
			}
			// The app is gone: the save-set's move is due, if it has not come already.
			gone = time.After(2 * time.Second)
		case <-gone:
			return nil
		case ev, ok := <-events:
			if !ok {
				return errors.New("the X connection closed")
			}
			switch ev := ev.(type) {
			case xproto.DestroyNotifyEvent:
				if ev.Window == c {
					return nil
				}
			case xproto.ReparentNotifyEvent:
				if ev.Window == c && ev.Parent != ctr && ev.Parent != root {
					xproto.ReparentWindow(conn, c, root, 0, 0)
					xproto.MapWindow(conn, c)
					conn.Sync()
					return nil
				}
			}
		}
	}
}

// GuardMain runs Guard from a guard process's arguments (the child's and the container's ids)
// with its standard input and output; it exits with the error, if any.
func GuardMain(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: embed-guard <window> <container>")
	}
	child, err1 := strconv.ParseUint(args[0], 10, 32)
	ctr, err2 := strconv.ParseUint(args[1], 10, 32)
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	return Guard("", uint32(child), uint32(ctr), os.Stdin, os.Stdout)
}
