// fakereims stands in for a Reims vGPU QEMU in scripts/check-gnome.sh (never shipped). Run as
// qemu-system-x86_64 (a link named so) with the app's -qmp argument and apple-gfx-pci, it writes the
// run's run.json like the app's run owner, opens a "Reims vGPU" X11 window with its _NET_WM_PID,
// and serves nothing; it exits 3 if its window is destroyed (as QEMU would on the BadWindow).
//
//	fakereims where <title>: prints "embedded", "desktop" or "gone" for that window.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func main() {
	xgb.Logger = log.New(io.Discard, "", 0)
	if len(os.Args) == 3 && os.Args[1] == "where" {
		fmt.Println(where(os.Args[2]))
		return
	}
	var qmp string
	for i, a := range os.Args {
		if a == "-qmp" && i+1 < len(os.Args) {
			qmp, _, _ = strings.Cut(strings.TrimPrefix(os.Args[i+1], "unix:"), ",")
		}
	}
	dir := filepath.Dir(qmp)
	if qmp == "" || os.MkdirAll(dir, 0o700) != nil {
		os.Exit(2)
	}
	g := make([]byte, 8)
	rand.Read(g)
	stat, _ := os.ReadFile("/proc/self/stat")
	f := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	ticks, _ := strconv.ParseUint(f[19], 10, 64)
	rec, _ := json.Marshal(map[string]any{"vm_id": os.Getenv("FAKE_VMID"), "pid": os.Getpid(), "start_ticks": ticks,
		"generation": hex.EncodeToString(g), "starter": "gui"})
	if os.WriteFile(filepath.Join(dir, "run.json"), rec, 0o600) != nil {
		os.Exit(2)
	}
	os.Remove(qmp)
	l, err := net.Listen("unix", qmp) // the socket exists; nothing answers on it
	if err != nil {
		os.Exit(2)
	}
	defer l.Close()
	conn, err := xgb.NewConn()
	if err != nil {
		os.Exit(2)
	}
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	w, _ := xproto.NewWindowId(conn)
	xproto.CreateWindow(conn, 0, w, root, 0, 0, 1024, 768, 0, xproto.WindowClassInputOutput, 0,
		xproto.CwBackPixel|xproto.CwEventMask, []uint32{0x3060a0, xproto.EventMaskStructureNotify})
	pid := make([]byte, 4)
	xgb.Put32(pid, uint32(os.Getpid()))
	xproto.ChangeProperty(conn, xproto.PropModeReplace, w, atom(conn, "_NET_WM_PID"), xproto.AtomCardinal, 32, 1, pid)
	title := "Reims vGPU (" + os.Getenv("FAKE_VMID") + ")"
	xproto.ChangeProperty(conn, xproto.PropModeReplace, w, xproto.AtomWmName, xproto.AtomString, 8, uint32(len(title)), []byte(title))
	xproto.MapWindow(conn, w)
	conn.Sync()
	for {
		ev, err := conn.WaitForEvent()
		if ev == nil && err == nil {
			os.Exit(4)
		}
		if d, ok := ev.(xproto.DestroyNotifyEvent); ok && d.Window == w {
			fmt.Fprintln(os.Stderr, "fakereims: my window was destroyed")
			os.Exit(3)
		}
	}
}

func atom(conn *xgb.Conn, name string) xproto.Atom {
	r, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0
	}
	return r.Atom
}

// where finds the window titled title: "embedded" when a kvm-gui container holds it, "desktop"
// when it is a window of its own (on the root or in a window manager's frame), "hidden" when it is
// not viewable, else "gone".
func where(title string) string {
	conn, err := xgb.NewConn()
	if err != nil {
		return "no X server"
	}
	defer conn.Close()
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	mark := atom(conn, "_KVM_GUI_CONTAINER")
	var find func(w xproto.Window, inside bool) (string, bool)
	find = func(w xproto.Window, inside bool) (string, bool) {
		t, err := xproto.QueryTree(conn, w).Reply()
		if err != nil {
			return "", false
		}
		for _, c := range t.Children {
			in := inside
			if p, err := xproto.GetProperty(conn, false, c, mark, xproto.GetPropertyTypeAny, 0, 0).Reply(); err == nil && p.Type != xproto.AtomNone {
				in = true
			}
			if n, err := xproto.GetProperty(conn, false, c, xproto.AtomWmName, xproto.GetPropertyTypeAny, 0, 256).Reply(); err == nil && string(n.Value) == title {
				if a, err := xproto.GetWindowAttributes(conn, c).Reply(); err != nil || a.MapState != xproto.MapStateViewable {
					return "hidden", true // on the desktop only when one can see it
				}
				if in {
					return "embedded", true
				}
				return "desktop", true
			}
			if s, ok := find(c, in); ok {
				return s, true
			}
		}
		return "", false
	}
	if s, ok := find(root, false); ok {
		return s
	}
	return "gone"
}
