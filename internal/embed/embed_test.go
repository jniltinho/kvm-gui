package embed

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// TestMain runs the test binary as a fake VM window (FAKE_X_CLIENT: a "Reims vGPU" window with
// its _NET_WM_PID, which exits 3 if its window is destroyed) or as a fake app that embeds one and
// waits to be killed (FAKE_X_APP), besides the tests.
func TestMain(m *testing.M) {
	switch {
	case os.Getenv("FAKE_X_GUARD") != "": // the guard the fake app starts (StartGuard)
		if err := GuardMain(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	case os.Getenv("FAKE_X_CLIENT") != "":
		fakeClient()
	case os.Getenv("FAKE_X_APP") != "":
		fakeApp()
	}
	os.Exit(m.Run())
}

func needX(t *testing.T) {
	t.Helper()
	if os.Getenv("DISPLAY") == "" {
		if os.Getenv("MKG_REQUIRE_X") == "1" {
			t.Fatal("no X server: run make test-embed")
		}
		t.Skip("no X server (make test-embed runs these under Xvfb)")
	}
}

// newWindow creates a mapped window on conn with _NET_WM_PID = pid and title.
func newWindow(conn *xgb.Conn, pid uint32, title string) (xproto.Window, error) {
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	w, err := xproto.NewWindowId(conn)
	if err != nil {
		return 0, err
	}
	if err := xproto.CreateWindowChecked(conn, 0, w, root, 0, 0, 640, 400, 0, xproto.WindowClassInputOutput, 0, 0, nil).Check(); err != nil {
		return 0, err
	}
	a, _ := xproto.InternAtom(conn, false, 11, "_NET_WM_PID").Reply()
	b := make([]byte, 4)
	xgb.Put32(b, pid)
	xproto.ChangeProperty(conn, xproto.PropModeReplace, w, a.Atom, xproto.AtomCardinal, 32, 1, b)
	xproto.ChangeProperty(conn, xproto.PropModeReplace, w, xproto.AtomWmName, xproto.AtomString, 8, uint32(len(title)), []byte(title))
	xproto.ChangeWindowAttributes(conn, w, xproto.CwEventMask, []uint32{xproto.EventMaskStructureNotify})
	xproto.MapWindow(conn, w)
	conn.Sync()
	return w, nil
}

func fakeClient() {
	conn, err := xgb.NewConn()
	if err != nil {
		os.Exit(2)
	}
	if _, err := newWindow(conn, uint32(os.Getpid()), "Reims vGPU (test)"); err != nil {
		os.Exit(2)
	}
	for {
		ev, err := conn.WaitForEvent()
		if ev == nil && err == nil {
			os.Exit(4) // the connection closed
		}
		if _, ok := ev.(xproto.DestroyNotifyEvent); ok {
			os.Exit(3)
		}
	}
}

// fakeApp is the app: its "GTK" connection makes the parent window and holds a save-set; it
// embeds the client given in FAKE_X_APP (pid) and prints "embedded", then waits to be killed.
func fakeApp() {
	pid, _ := strconv.Atoi(os.Getenv("FAKE_X_APP"))
	gtk, err := xgb.NewConn()
	if err != nil {
		os.Exit(2)
	}
	parent, err := newWindow(gtk, uint32(os.Getpid()), "kvm-gui")
	if err != nil {
		os.Exit(2)
	}
	x, err := Open("")
	if err != nil {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	child, err := x.FindWindow(ctx, uint32(pid), ReimsTitle)
	if err != nil {
		os.Exit(5)
	}
	e, err := x.Embed(parent, child, uint32(pid), func(w uint32) error {
		return xproto.ChangeSaveSetChecked(gtk, xproto.SetModeInsert, xproto.Window(w)).Check()
	})
	if err != nil {
		os.Exit(6)
	}
	e.SetRect(10, 20, 300, 200)
	if os.Getenv("FAKE_X_APP_GUARD") != "" {
		os.Setenv("FAKE_X_GUARD", "1")
		if err := e.StartGuard(os.Args[0]); err != nil {
			os.Exit(7)
		}
	}
	fmt.Println("embedded")
	select {}
}

func startClient(t *testing.T) *exec.Cmd {
	t.Helper()
	c := exec.Command(os.Args[0])
	c.Env = append(os.Environ(), "FAKE_X_CLIENT=1")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Process.Kill(); c.Wait() })
	return c
}

func parentOf(t *testing.T, conn *xgb.Conn, w xproto.Window) xproto.Window {
	t.Helper()
	tree, err := xproto.QueryTree(conn, w).Reply()
	if err != nil {
		t.Fatalf("window %d: %v", w, err)
	}
	return tree.Parent
}

// Find, embed, place, follow a self-resize, hide and show, release; a stale pid is refused; two
// embedded windows are independent (task 6.4).
func TestEmbed(t *testing.T) {
	needX(t)
	x, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	app, _ := xgb.NewConn()
	defer app.Close()
	parent, err := newWindow(app, uint32(os.Getpid()), "kvm-gui")
	if err != nil {
		t.Fatal(err)
	}
	c1, c2 := startClient(t), startClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	w1, err := x.FindWindow(ctx, uint32(c1.Process.Pid), ReimsTitle)
	if err != nil {
		t.Fatal(err)
	}
	short, scancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	if _, err := x.FindWindow(short, uint32(c1.Process.Pid), "QEMU"); err == nil {
		t.Fatal("found a window by the wrong title")
	}
	scancel()
	if _, err := x.Embed(parent, w1, uint32(c2.Process.Pid), nil); err == nil {
		t.Fatal("embedded a window of another pid")
	}
	e1, err := x.Embed(parent, w1, uint32(c1.Process.Pid), nil)
	if err != nil {
		t.Fatal(err)
	}
	e1.SetRect(10, 20, 300, 200)
	container := parentOf(t, app, w1)
	if parentOf(t, app, container) != parent {
		t.Fatal("not inside the app's window")
	}
	g, _ := xproto.GetGeometry(app, xproto.Drawable(container)).Reply()
	if g.X != 10 || g.Y != 20 || g.Width != 300 || g.Height != 200 {
		t.Fatalf("container geometry %+v", g)
	}
	// The VM resizes its own window (a guest mode change): Pump puts it back.
	xproto.ConfigureWindow(app, w1, xproto.ConfigWindowWidth|xproto.ConfigWindowHeight, []uint32{800, 600})
	app.Sync()
	time.Sleep(100 * time.Millisecond)
	e1.Pump()
	time.Sleep(100 * time.Millisecond)
	if g, _ := xproto.GetGeometry(app, xproto.Drawable(w1)).Reply(); g.Width != 300 || g.Height != 200 {
		t.Fatalf("self-resize not undone: %dx%d", g.Width, g.Height)
	}
	e1.Hide()
	if a, _ := xproto.GetWindowAttributes(app, w1).Reply(); a.MapState == xproto.MapStateViewable {
		t.Fatal("hidden but viewable")
	}
	e1.Show()
	// A second VM in a second app window; releasing the first leaves the second embedded.
	parent2, _ := newWindow(app, uint32(os.Getpid()), "kvm-gui 2")
	w2, err := x.FindWindow(ctx, uint32(c2.Process.Pid), ReimsTitle)
	if err != nil {
		t.Fatal(err)
	}
	e2, err := x.Embed(parent2, w2, uint32(c2.Process.Pid), nil)
	if err != nil {
		t.Fatal(err)
	}
	e2.SetRect(0, 0, 200, 100)
	if _, err := x.FindWindow(short, uint32(c2.Process.Pid), ReimsTitle); err == nil {
		t.Fatal("found a window inside a container")
	}
	e1.Release()
	if parentOf(t, app, w1) != xproto.Setup(app).DefaultScreen(app).Root || parentOf(t, app, parentOf(t, app, w2)) != parent2 {
		t.Fatal("release: the first is on the desktop, the second stays embedded")
	}
	if c1.ProcessState != nil {
		t.Fatal("the first VM window was destroyed")
	}
	e2.Release()
}

// The app is killed with SIGKILL while a VM window is embedded: the save-set brings the window
// back to the desktop and the VM keeps it (task 6.4).
func TestSurvivesSIGKILL(t *testing.T) {
	needX(t)
	client := startClient(t)
	app := exec.Command(os.Args[0])
	app.Env = append(os.Environ(), "FAKE_X_APP="+strconv.Itoa(client.Process.Pid))
	out, _ := app.StdoutPipe()
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	if n, _ := out.Read(buf); string(buf[:n]) != "embedded\n" {
		app.Process.Kill()
		t.Fatalf("the fake app did not embed: %q", buf[:n])
	}
	app.Process.Signal(syscall.SIGKILL)
	app.Wait()
	time.Sleep(300 * time.Millisecond)
	x, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	w, err := x.FindWindow(ctx, uint32(client.Process.Pid), ReimsTitle)
	if err != nil {
		t.Fatalf("the VM window did not survive: %v (client exited: %v)", err, client.ProcessState)
	}
	if p := parentOf(t, x.conn, w); p != x.root {
		t.Fatalf("parent %d, want the root", p)
	}
}

// teardown is how long fakeWM takes to destroy the frame of a window that was destroyed.
const teardown = 30 * time.Millisecond

// fakeWM is a reparenting window manager like KWin: it frames every top-level window mapped on
// the root, and on unmap puts the window back on the root and destroys the frame. stop ends it.
func fakeWM(t *testing.T) (stop func()) {
	t.Helper()
	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	if err := xproto.ChangeWindowAttributesChecked(conn, root, xproto.CwEventMask,
		[]uint32{xproto.EventMaskSubstructureRedirect | xproto.EventMaskSubstructureNotify}).Check(); err != nil {
		t.Fatalf("another window manager runs: %v", err)
	}
	frames := map[xproto.Window]xproto.Window{} // client → frame
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			ev, err := conn.WaitForEvent()
			if ev == nil && err == nil {
				return
			}
			switch ev := ev.(type) {
			case xproto.MapRequestEvent:
				f, _ := xproto.NewWindowId(conn)
				xproto.CreateWindow(conn, 0, f, root, 0, 0, 1000, 800, 0, xproto.WindowClassInputOutput, 0, 0, nil)
				xproto.ChangeWindowAttributes(conn, f, xproto.CwEventMask, []uint32{xproto.EventMaskSubstructureNotify})
				xproto.ChangeSaveSet(conn, xproto.SetModeInsert, ev.Window)
				xproto.ReparentWindow(conn, ev.Window, f, 4, 20)
				xproto.MapWindow(conn, f)
				xproto.MapWindow(conn, ev.Window)
				frames[ev.Window] = f
			case xproto.ConfigureRequestEvent:
				xproto.ConfigureWindow(conn, ev.Window, xproto.ConfigWindowWidth|xproto.ConfigWindowHeight, []uint32{uint32(ev.Width), uint32(ev.Height)})
			case xproto.DestroyNotifyEvent:
				// Its client is gone: the frame goes, a moment later, like a real window manager's
				// (several round trips: mutter's frames client, KWin's close effect).
				if f, ok := frames[ev.Window]; ok {
					delete(frames, ev.Window)
					time.AfterFunc(teardown, func() { xproto.DestroyWindow(conn, f); conn.Sync() })
				}
			case xproto.UnmapNotifyEvent:
				if f, ok := frames[ev.Window]; ok && ev.Event == f {
					delete(frames, ev.Window)
					xproto.ReparentWindow(conn, ev.Window, root, 0, 0)
					xproto.ChangeSaveSet(conn, xproto.SetModeDelete, ev.Window)
					xproto.DestroyWindow(conn, f)
				}
			}
			conn.Sync()
		}
	}()
	return func() { conn.Close(); <-done }
}

// Under a reparenting window manager the move's own ReparentNotify to the root must not mark the
// window lost: the embedder would then destroy the container with QEMU's window in it, and QEMU
// dies of the BadWindow (seen on KDE).
func TestEmbedUnderWindowManager(t *testing.T) {
	needX(t)
	stop := fakeWM(t)
	defer stop()
	app, _ := xgb.NewConn()
	defer app.Close()
	parent, err := newWindow(app, uint32(os.Getpid()), "kvm-gui")
	if err != nil {
		t.Fatal(err)
	}
	client := startClient(t)
	x, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	w, err := x.FindWindow(ctx, uint32(client.Process.Pid), ReimsTitle)
	if err != nil {
		t.Fatal(err)
	}
	root := x.root
	if parentOf(t, app, w) == root {
		t.Fatal("the fake window manager did not frame the VM's window")
	}
	e, err := x.Embed(parent, w, uint32(client.Process.Pid), nil)
	if err != nil {
		t.Fatal(err)
	}
	e.SetRect(0, 0, 300, 200)
	for range 10 {
		e.Pump()
		time.Sleep(50 * time.Millisecond)
	}
	if !e.Alive() {
		t.Fatal("the window manager's move marked the window lost")
	}
	if container := parentOf(t, app, w); parentOf(t, app, container) != parent {
		t.Fatal("not inside the app's window")
	}
	// Released even when the embedder thinks it is lost: the window goes back, never destroyed.
	e.alive = false
	e.Release()
	if p := parentOf(t, app, w); p == e.container {
		t.Fatal("still in the container")
	}
}

// The app is killed with SIGKILL under a reparenting window manager: the save-set moves the VM's
// window into the frame of the app's window, which the window manager destroys. Without a guard
// the VM loses its window (QEMU dies of it); the guard puts it back on the desktop first.
func TestSIGKILLUnderWindowManager(t *testing.T) {
	needX(t)
	stop := fakeWM(t)
	defer stop()
	x, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	for _, guarded := range []bool{false, true} {
		client := startClient(t)
		app := exec.Command(os.Args[0])
		app.Env = append(os.Environ(), "FAKE_X_APP="+strconv.Itoa(client.Process.Pid))
		if guarded {
			app.Env = append(app.Env, "FAKE_X_APP_GUARD=1")
		}
		out, _ := app.StdoutPipe()
		if err := app.Start(); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 16)
		if n, _ := out.Read(buf); string(buf[:n]) != "embedded\n" {
			app.Process.Kill()
			app.Wait()
			t.Fatalf("guarded %v: the fake app did not embed: %q", guarded, buf[:n])
		}
		app.Process.Signal(syscall.SIGKILL)
		app.Wait()
		time.Sleep(10 * teardown)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		_, err := x.FindWindow(ctx, uint32(client.Process.Pid), ReimsTitle)
		cancel()
		if guarded && err != nil {
			t.Fatalf("with a guard the VM lost its window: %v", err)
		}
		if !guarded && err == nil {
			t.Fatal("without a guard the window survived: the fake window manager does not destroy frames")
		}
	}
}
