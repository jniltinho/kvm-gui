package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/jniltinho/kvm-gui/internal/core"
	"github.com/jniltinho/kvm-gui/internal/embed"
)

// A Reims vGPU VM started by this app opens its window on X11 (EmbedX11); its VM window takes that
// window over the screen area (design D6), and gives it back to the desktop before it closes or the
// app exits. Runs this app cannot embed (the CLI's, the MCP server's, Wayland) use the viewer.

// findTimeout: a Reims window that has not appeared by then stays a window of its own.
const findTimeout = 30 * time.Second

// vmEmbed is a VM's window held by one window of the app (its host).
type vmEmbed struct {
	host    string // "manager" (its Screen tab) or "vm" (the VM's window)
	e       *embed.Embedded
	release func()        // the gui-run.lock claim
	stop    chan struct{} // ends the pump
	done    chan struct{} // the pump has ended
}

// lookup is a running search for a run's window, for one host.
type lookup struct {
	host   string
	cancel context.CancelFunc
	done   chan struct{}
}

type embedder struct {
	mu      sync.Mutex
	x       *embed.X
	vms     map[string]*vmEmbed
	pending map[string]*lookup
	failed  map[string]failure // the lookup for this run failed: the viewer, no new tries
	told    map[string]string  // the last state each page got ("<id>/<host>"), for the log
}

type failure struct {
	generation string
	reason     string
}

var embeds = &embedder{vms: map[string]*vmEmbed{}, pending: map[string]*lookup{}, failed: map[string]failure{}, told: map[string]string{}}

// logf appends a line to StateDir/logs/embed.log: what embedding did on the user's desktop.
func logf(format string, a ...any) {
	p := filepath.Join(core.StateDir(), "logs", "embed.log")
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	if st, err := os.Stat(p); err == nil && st.Size() > 1<<20 { // one older file is kept
		os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), fmt.Sprintf(format, a...))
}

// EmbedState is what the page shows in a Reims VM's screen area.
type EmbedState struct {
	State  string `json:"state"`  // "embedded", "waiting", "elsewhere" (the other window holds it) or "unavailable"
	Reason string `json:"reason"` // why unavailable: the page uses the viewer then
}

// EmbedScreen puts id's Reims vGPU window over the page rectangle (CSS pixels from the page's
// top-left, dpr = devicePixelRatio) in host, the Manager ("manager", its Screen tab) or the VM's
// window ("vm"), starting the lookup the first time. take: this host takes it from the other one
// (the page asks so when it shows the screen, not when it only re-places it). The page calls it
// again on every resize and every second while embedded; it uses the in-app viewer when the
// state is "unavailable".
func (a *App) EmbedScreen(id, host string, take bool, x, y, w, h, dpr float64) (EmbedState, error) {
	st, err := a.embedScreen(id, host, take, x, y, w, h, dpr)
	embeds.mu.Lock()
	k := id + "/" + host
	changed := embeds.told[k] != st.State
	embeds.told[k] = st.State
	embeds.mu.Unlock()
	if changed {
		logf("%s: the %s window shows %s %s", id, host, st.State, st.Reason)
	}
	return st, err
}

func (a *App) embedScreen(id, host string, take bool, x, y, w, h, dpr float64) (EmbedState, error) {
	var win *application.WebviewWindow
	switch {
	case host == "manager" && a.mainWindow != nil:
		win = a.mainWindow()
	case host == "vm" && a.vmWindow != nil:
		win = a.vmWindow(id)
	}
	if win == nil || !onX11() {
		logf("%s: not embedded in the %s window: the app is not in an X11 session", id, host)
		return EmbedState{"unavailable", "the app is not in an X11 session"}, nil
	}
	embeds.mu.Lock()
	v, p := embeds.vms[id], embeds.pending[id]
	embeds.mu.Unlock()
	if (v != nil && v.host != host) || (p != nil && p.host != host) {
		if !take {
			return EmbedState{State: "elsewhere"}, nil
		}
		embeds.cancel(id, p)
		embeds.dropIf(id, v)
		v, p = nil, nil
	}
	if v != nil {
		if !v.e.Alive() {
			logf("%s: the VM's window is gone", id)
			embeds.dropIf(id, v)
			return EmbedState{"unavailable", "the VM's window is gone"}, nil
		}
		if w < 2 || h < 2 { // no size yet (or hidden): the next call places it
			return EmbedState{State: "embedded"}, nil
		}
		ox, oy, scale := webviewOrigin(win)
		rx, ry, rw, rh := int16(ox*float64(scale)+x*dpr), int16(oy*float64(scale)+y*dpr), uint16(w*dpr), uint16(h*dpr)
		if v.e.SetRect(rx, ry, rw, rh) {
			logf("%s: placed at %d,%d %dx%d in %s (origin %.0f,%.0f scale %d, page %.0f,%.0f %.0fx%.0f dpr %.2f)", id, rx, ry, rw, rh, host, ox, oy, scale, x, y, w, h, dpr)
		}
		return EmbedState{State: "embedded"}, nil
	}
	if p != nil {
		return EmbedState{State: "waiting"}, nil
	}
	// The lookup is registered before the run is claimed: a second call meanwhile (the page asks
	// on mount and on resize at once) waits for it instead of finding the claim taken.
	ctx, cancel := context.WithTimeout(context.Background(), findTimeout)
	l := &lookup{host: host, cancel: cancel, done: make(chan struct{})}
	embeds.mu.Lock()
	if embeds.vms[id] != nil || embeds.pending[id] != nil {
		embeds.mu.Unlock()
		cancel()
		return EmbedState{State: "waiting"}, nil
	}
	embeds.pending[id] = l
	embeds.mu.Unlock()
	unavailable := func(reason string) (EmbedState, error) {
		embeds.mu.Lock()
		if embeds.pending[id] == l {
			delete(embeds.pending, id)
		}
		embeds.mu.Unlock()
		cancel()
		close(l.done)
		return EmbedState{"unavailable", reason}, nil
	}
	e, err := entry(id)
	if err != nil {
		return unavailable(err.Error())
	}
	run, release, err := core.ClaimGUIRun(e)
	if err != nil {
		return unavailable(err.Error())
	}
	if !run.Reims {
		release()
		return unavailable("not a Reims vGPU run")
	}
	embeds.mu.Lock()
	f, failed := embeds.failed[id]
	embeds.mu.Unlock()
	if failed && f.generation == run.Generation {
		release()
		return unavailable(f.reason)
	}
	go embeds.take(ctx, l, id, e, win, run, release)
	return EmbedState{State: "waiting"}, nil
}

// take finds the run's Reims window and embeds it into win, unless the lookup was cancelled (its
// window closed, another host took over) or the run changed meanwhile; the page's next
// EmbedScreen places it.
func (m *embedder) take(ctx context.Context, l *lookup, id string, entry core.VMEntry, win *application.WebviewWindow, run core.RunInfo, release func()) {
	defer close(l.done)
	defer l.cancel()
	pid := uint32(run.PID)
	end := func(err error) {
		release()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.pending[id] == l {
			delete(m.pending, id)
		}
		if err != nil && ctx.Err() != context.Canceled {
			logf("%s: not embedded (pid %d): %v", id, pid, err)
			m.failed[id] = failure{run.Generation, "Reims vGPU's window could not be taken into the app: " + err.Error()}
		}
	}
	logf("%s: looking for Reims vGPU's window of pid %d for the %s window", id, pid, l.host)
	m.mu.Lock()
	if m.x == nil {
		x, err := embed.Open("")
		if err != nil {
			m.mu.Unlock()
			end(err)
			return
		}
		m.x = x
	}
	x := m.x
	m.mu.Unlock()
	child, err := x.FindWindow(ctx, pid, embed.ReimsTitle)
	if err != nil {
		end(err)
		return
	}
	parent := windowXID(win)
	if parent == 0 {
		end(fmt.Errorf("the app's window has no X11 window"))
		return
	}
	e, err := x.Embed(xproto.Window(parent), child, pid, gtkSaveSet)
	if err != nil {
		end(err)
		return
	}
	now, running := core.FindRun(entry)
	m.mu.Lock()
	if m.pending[id] != l || ctx.Err() != nil || !running || now.Generation != run.Generation {
		m.mu.Unlock()
		e.Release() // its window closed, another host took over, or another run: back to the desktop
		end(nil)
		return
	}
	v := &vmEmbed{host: l.host, e: e, release: release, stop: make(chan struct{}), done: make(chan struct{})}
	m.vms[id] = v
	delete(m.pending, id)
	m.mu.Unlock()
	logf("%s: window %#x embedded into %#x", id, child, parent)
	go func() { // keep it in place; a window that died or was taken away is let go
		defer close(v.done)
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-v.stop:
				return
			case <-t.C:
				v.e.Pump()
				if !v.e.Alive() {
					logf("%s: the VM's window was destroyed or taken away", id)
					go m.dropIf(id, v) // it waits for this pump to end
					return
				}
			}
		}
	}()
}

// EmbedVisible hides the VM's window while an HTML menu or dialog of host covers the screen area,
// or host stops showing it; only the host holding it is heard.
func (a *App) EmbedVisible(id, host string, visible bool) {
	embeds.mu.Lock()
	v := embeds.vms[id]
	embeds.mu.Unlock()
	if v == nil || v.host != host {
		return
	}
	if visible {
		v.e.Show()
	} else {
		v.e.Hide()
	}
}

// cancel ends a pending lookup and waits for it.
func (m *embedder) cancel(id string, l *lookup) {
	if l == nil {
		return
	}
	l.cancel()
	<-l.done
}

// dropIf gives v's window back to the desktop as a window of its own, if v still holds id's
// window; the run goes on.
func (m *embedder) dropIf(id string, v *vmEmbed) {
	m.mu.Lock()
	if v == nil || m.vms[id] != v {
		m.mu.Unlock()
		return
	}
	delete(m.vms, id)
	m.mu.Unlock()
	close(v.stop)
	<-v.done
	v.e.Release()
	v.release()
	logf("%s: given back to the desktop", id)
}

// dropHost lets go of what host holds or is looking for: before that window closes. id "" is
// every VM (the Manager).
func (m *embedder) dropHost(id, host string) {
	m.mu.Lock()
	var vs []*vmEmbed
	var ids []string
	var ls []*lookup
	for k, v := range m.vms {
		if (id == "" || k == id) && v.host == host {
			vs, ids = append(vs, v), append(ids, k)
		}
	}
	for k, l := range m.pending {
		if (id == "" || k == id) && l.host == host {
			ls = append(ls, l)
		}
	}
	m.mu.Unlock()
	for _, l := range ls {
		m.cancel("", l)
	}
	for i, v := range vs {
		m.dropIf(ids[i], v)
	}
}

// releaseAll lets every embedded window go: before the app exits (signals included).
func (m *embedder) releaseAll() {
	m.dropHost("", "manager")
	m.dropHost("", "vm")
}
