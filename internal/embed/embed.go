// Package embed puts a running VM's own X11 window (Reims vGPU's SDL3 window) inside a window of
// the app, in pure Go over the X protocol (design D6). It never imports GTK: the one call that
// needs GTK's own X connection, the save-set, is handed in by the GUI.
package embed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// ReimsTitle is how Reims vGPU's window title starts (the library appends to it).
const ReimsTitle = "Reims vGPU"

// containerMark is set on every container: a lookup never takes a window another embedder
// (this app's other window, or another instance) holds.
const containerMark = "_KVM_GUI_CONTAINER"

// wmRelease is how long Embed waits for a reparenting window manager to let the window go.
const wmRelease = time.Second

// releaseGrace bounds how long Release keeps the old container after giving the window back.
const releaseGrace = time.Second

// X is a connection to the X server, for finding and embedding windows.
type X struct {
	display string
	conn    *xgb.Conn
	root    xproto.Window
	atoms   map[string]xproto.Atom
}

// Open connects to display ("" = $DISPLAY).
func Open(display string) (*X, error) {
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, fmt.Errorf("X11: %w", err)
	}
	x := &X{display: display, conn: conn, root: xproto.Setup(conn).DefaultScreen(conn).Root, atoms: map[string]xproto.Atom{}}
	for _, name := range []string{"_NET_WM_PID", "_NET_WM_NAME", "UTF8_STRING", "_NET_WM_STATE", containerMark, "_NET_ACTIVE_WINDOW"} {
		r, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			conn.Close()
			return nil, err
		}
		x.atoms[name] = r.Atom
	}
	return x, nil
}

func (x *X) Close() { x.conn.Close() }

// FindWindow waits (polling every 50 ms) until ctx ends for a viewable window whose _NET_WM_PID
// is pid and whose title starts with prefix, anywhere in the tree (window-manager frames are fine)
// but never inside a container.
func (x *X) FindWindow(ctx context.Context, pid uint32, prefix string) (xproto.Window, error) {
	for {
		if w, ok := x.scan(x.root, pid, prefix); ok {
			return w, nil
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("no window of pid %d titled %q: %w", pid, prefix, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (x *X) scan(w xproto.Window, pid uint32, prefix string) (xproto.Window, bool) {
	tree, err := xproto.QueryTree(x.conn, w).Reply()
	if err != nil { // windows vanish between requests: not it
		return 0, false
	}
	for _, c := range tree.Children {
		if x.hasProp(c, containerMark) {
			continue
		}
		if x.pidOf(c) == pid && x.viewable(c) && strings.HasPrefix(x.title(c), prefix) {
			return c, true
		}
		if found, ok := x.scan(c, pid, prefix); ok {
			return found, true
		}
	}
	return 0, false
}

func (x *X) hasProp(w xproto.Window, name string) bool {
	r, err := xproto.GetProperty(x.conn, false, w, x.atoms[name], xproto.GetPropertyTypeAny, 0, 0).Reply()
	return err == nil && r.Type != xproto.AtomNone
}

func (x *X) pidOf(w xproto.Window) uint32 {
	r, err := xproto.GetProperty(x.conn, false, w, x.atoms["_NET_WM_PID"], xproto.AtomCardinal, 0, 1).Reply()
	if err != nil || r.Format != 32 || len(r.Value) < 4 {
		return 0
	}
	return xgb.Get32(r.Value)
}

func (x *X) viewable(w xproto.Window) bool {
	a, err := xproto.GetWindowAttributes(x.conn, w).Reply()
	return err == nil && a.MapState == xproto.MapStateViewable
}

func (x *X) title(w xproto.Window) string {
	for _, p := range [][2]xproto.Atom{{x.atoms["_NET_WM_NAME"], x.atoms["UTF8_STRING"]}, {xproto.AtomWmName, xproto.GetPropertyTypeAny}} {
		r, err := xproto.GetProperty(x.conn, false, w, p[0], p[1], 0, 1024).Reply()
		if err == nil && len(r.Value) > 0 {
			return string(r.Value)
		}
	}
	return ""
}

// Embedded is a VM's window inside a window of the app. Release it before that window closes or
// the app exits; the save-set brings it back to the desktop if the app dies first.
type Embedded struct {
	x                        *X
	conn                     *xgb.Conn // its own connection: it owns the container
	parent, container, child xproto.Window
	pid                      uint32
	x0, y0                   int16
	w, h                     uint16
	alive, released, shown   bool
	guard                    *exec.Cmd // StartGuard's process
	guardIn                  io.WriteCloser
}

// Embed reparents child (whose _NET_WM_PID must still be pid: a window re-used by another
// process is never taken) into a new container inside parent, unmapped until SetRect. saveSet
// adds child to the save-set of the connection that created parent (GTK's), so the X server
// puts it back on the root when the app dies; the container's own connection adds it too.
func (x *X) Embed(parent, child xproto.Window, pid uint32, saveSet func(uint32) error) (*Embedded, error) {
	if x.pidOf(child) != pid {
		return nil, errors.New("the window no longer belongs to the VM's QEMU")
	}
	conn, err := xgb.NewConnDisplay(x.display)
	if err != nil {
		return nil, err
	}
	e := &Embedded{x: x, conn: conn, parent: parent, child: child, pid: pid, alive: true}
	fail := func(err error) (*Embedded, error) { conn.Close(); return nil, err }
	if e.container, err = xproto.NewWindowId(conn); err != nil {
		return fail(err)
	}
	// No background: the server never clears it, so resizes do not flash.
	if err := xproto.CreateWindowChecked(conn, 0, e.container, parent, 0, 0, 1, 1, 0, xproto.WindowClassInputOutput, 0,
		xproto.CwBackPixmap, []uint32{xproto.BackPixmapNone}).Check(); err != nil {
		return fail(err)
	}
	mark := make([]byte, 4)
	xgb.Put32(mark, uint32(os.Getpid()))
	if err := xproto.ChangePropertyChecked(conn, xproto.PropModeReplace, e.container, x.atoms[containerMark], xproto.AtomCardinal, 32, 1, mark).Check(); err != nil {
		return fail(err)
	}
	if err := xproto.ChangeSaveSetChecked(conn, xproto.SetModeInsert, child).Check(); err != nil {
		return fail(err)
	}
	if saveSet != nil {
		if err := saveSet(uint32(child)); err != nil {
			return fail(fmt.Errorf("save-set: %w", err))
		}
	}
	// StructureNotify: resizes, destruction and another client taking it; EnterWindow: the
	// pointer entering gives it the keyboard (SDL holds ButtonPress, so clicks cannot be watched).
	if err := xproto.ChangeWindowAttributesChecked(conn, child, xproto.CwEventMask,
		[]uint32{xproto.EventMaskStructureNotify | xproto.EventMaskEnterWindow}).Check(); err != nil {
		return fail(err)
	}
	xproto.ChangeWindowAttributes(conn, parent, xproto.CwEventMask, []uint32{xproto.EventMaskFocusChange})
	// Unmapped first so a window manager lets go of it, and without fullscreen/maximized state.
	tree, err := xproto.QueryTree(conn, child).Reply()
	if err != nil {
		return fail(err)
	}
	framed := tree.Parent != x.root
	xproto.UnmapWindow(conn, child)
	xproto.DeleteProperty(conn, child, x.atoms["_NET_WM_STATE"])
	for deadline := time.Now().Add(wmRelease); framed && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if t, err := xproto.QueryTree(conn, child).Reply(); err != nil || t.Parent == x.root {
			break
		}
	}
	if x.pidOf(child) != pid { // the stale-lookup guard, once more right before taking it
		return fail(errors.New("the window no longer belongs to the VM's QEMU"))
	}
	if err := xproto.ReparentWindowChecked(conn, child, e.container, 0, 0).Check(); err != nil {
		return fail(err)
	}
	if err := xproto.MapWindowChecked(conn, child).Check(); err != nil {
		return fail(err)
	}
	// Events so far come from the move (a window manager's ReparentNotify to the root among
	// them), not from the child's life: where it is now is what counts.
	e.drain()
	if !e.inContainer() {
		return fail(errors.New("the window manager took the VM's window back"))
	}
	e.alive = true
	return e, nil
}

// drain reads pending events: a destroyed child, or one another client reparented away, is lost.
func (e *Embedded) drain() (configured bool) {
	for {
		ev, err := e.conn.PollForEvent()
		if ev == nil && err == nil {
			return configured
		}
		switch ev := ev.(type) {
		case xproto.DestroyNotifyEvent:
			if ev.Window == e.child {
				e.alive = false
			}
		case xproto.ReparentNotifyEvent:
			if ev.Window == e.child && ev.Parent != e.container {
				e.alive = false
			}
		case xproto.ConfigureNotifyEvent:
			if ev.Window == e.child && (ev.X != 0 || ev.Y != 0 || ev.Width != e.w || ev.Height != e.h) {
				configured = true
			}
		case xproto.EnterNotifyEvent:
			if ev.Event == e.child && e.alive && e.appActive() {
				xproto.SetInputFocus(e.conn, xproto.InputFocusParent, e.child, xproto.TimeCurrentTime)
			}
		}
	}
}

// SetRect places the VM's window over the page's screen rectangle (physical pixels relative to
// the app's window) and shows it; it sends nothing and returns false when nothing changed.
func (e *Embedded) SetRect(x, y int16, w, h uint16) bool {
	w, h = max(w, 1), max(h, 1) // X refuses zero sizes
	if e.shown && x == e.x0 && y == e.y0 && w == e.w && h == e.h {
		return false
	}
	e.x0, e.y0 = x, y
	xproto.ConfigureWindow(e.conn, e.container, xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
		[]uint32{uint32(x), uint32(y), uint32(w), uint32(h)})
	e.w, e.h = w, h
	e.configureChild()
	if !e.shown {
		e.Show()
	}
	return true
}

func (e *Embedded) configureChild() {
	if e.alive {
		xproto.ConfigureWindow(e.conn, e.child, xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, 0, uint32(e.w), uint32(e.h)})
	}
}

// Pump handles pending events without blocking: a child that moved or resized itself (a guest
// mode change) is put back over the rectangle; call it often (each frame or a short ticker).
func (e *Embedded) Pump() {
	if e.drain() {
		e.configureChild()
	}
	e.conn.Sync()
}

// Show and Hide map and unmap it: hidden while an HTML dialog or menu covers the screen.
func (e *Embedded) Show() { xproto.MapWindow(e.conn, e.container); e.shown = true; e.conn.Sync() }
func (e *Embedded) Hide() { xproto.UnmapWindow(e.conn, e.container); e.shown = false; e.conn.Sync() }

// Focus gives the VM's window the keyboard; Unfocus gives it back to the app's window.
func (e *Embedded) Focus() error {
	return xproto.SetInputFocusChecked(e.conn, xproto.InputFocusParent, e.child, xproto.TimeCurrentTime).Check()
}
func (e *Embedded) Unfocus() error {
	return xproto.SetInputFocusChecked(e.conn, xproto.InputFocusParent, e.parent, xproto.TimeCurrentTime).Check()
}

// inContainer: the VM's window still exists and is in the container.
func (e *Embedded) inContainer() bool {
	t, err := xproto.QueryTree(e.conn, e.child).Reply()
	return err == nil && t.Parent == e.container
}

// Alive is false once QEMU's window is gone or another client took it.
func (e *Embedded) Alive() bool { return e.alive }

// Child is the VM's window.
func (e *Embedded) Child() xproto.Window { return e.child }

// appActive: the app's window is the active one (_NET_ACTIVE_WINDOW, else the keyboard focus is
// inside it), so hovering the VM while another application is in front never steals the keyboard.
func (e *Embedded) appActive() bool {
	r, err := xproto.GetProperty(e.conn, false, e.x.root, e.x.atoms["_NET_ACTIVE_WINDOW"], xproto.AtomWindow, 0, 1).Reply()
	if err == nil && r.Type != xproto.AtomNone && len(r.Value) >= 4 {
		return xproto.Window(xgb.Get32(r.Value)) == e.parent
	}
	f, err := xproto.GetInputFocus(e.conn).Reply()
	if err != nil {
		return false
	}
	for w := f.Focus; w > 1 && w != e.x.root; {
		if w == e.parent {
			return true
		}
		t, err := xproto.QueryTree(e.conn, w).Reply()
		if err != nil {
			return false
		}
		w = t.Parent
	}
	return false
}

// Release puts a live VM window back on the desktop as a window of its own and removes the
// container; the run goes on. Safe to call twice.
func (e *Embedded) Release() error {
	if e.released {
		return nil
	}
	e.released = true
	e.stopGuard()
	e.drain()
	// Never trust alive here: destroying a container that still holds the VM's window destroys
	// that window, and QEMU dies of the BadWindow.
	if e.inContainer() {
		e.alive = false
		xproto.UnmapWindow(e.conn, e.child)
		xproto.ReparentWindow(e.conn, e.child, e.x.root, 0, 0)
		xproto.ChangeSaveSet(e.conn, xproto.SetModeDelete, e.child)
		xproto.MapWindow(e.conn, e.child)
		e.conn.Sync()
		// The VM's toolkit (SDL) still queries its former parent while it handles the move: that
		// window must outlive the move, or the query's BadWindow kills QEMU. Wait for the move,
		// then give the client a moment before the container goes.
		for deadline := time.Now().Add(releaseGrace); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if t, err := xproto.QueryTree(e.conn, e.child).Reply(); err != nil || t.Parent == e.x.root {
				break
			}
		}
		time.Sleep(releaseGrace / 2)
	}
	xproto.UnmapWindow(e.conn, e.container)
	xproto.DestroyWindow(e.conn, e.container)
	e.conn.Sync()
	e.conn.Close()
	return nil
}
