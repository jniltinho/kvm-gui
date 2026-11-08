package gui

import (
	"context"
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

type vmEmbed struct {
	host    string // "manager" or "vm": the window it is in
	e       *embed.Embedded
	release func() // the gui-run.lock claim
	stop    chan struct{}
}

type embedder struct {
	mu   sync.Mutex
	x    *embed.X
	vms  map[string]*vmEmbed
	busy map[string]bool // a lookup is running
}

var embeds = &embedder{vms: map[string]*vmEmbed{}, busy: map[string]bool{}}

// EmbedState is what the page shows in a Reims VM's screen area.
type EmbedState struct {
	State  string `json:"state"`  // "embedded", "waiting" or "unavailable"
	Reason string `json:"reason"` // why unavailable: the page uses the viewer then
}

// EmbedScreen puts id's Reims vGPU window over the page rectangle (CSS pixels from the page's
// top-left, dpr = devicePixelRatio) in host, the Manager ("manager", its Screen tab) or the VM's
// window ("vm"), starting the lookup the first time and moving it when another host asks; call it
// again on every resize. The page uses the in-app viewer when the state is "unavailable".
func (a *App) EmbedScreen(id, host string, x, y, w, h, dpr float64) (EmbedState, error) {
	var win *application.WebviewWindow
	switch {
	case host == "manager" && a.mainWindow != nil:
		win = a.mainWindow()
	case host == "vm" && a.vmWindow != nil:
		win = a.vmWindow(id)
	}
	if win == nil || !onX11() {
		return EmbedState{"unavailable", "the app is not on X11"}, nil
	}
	embeds.mu.Lock()
	v := embeds.vms[id]
	busy := embeds.busy[id]
	embeds.mu.Unlock()
	if v != nil && v.host != host { // the other window takes it: back to the desktop, then in
		embeds.drop(id)
		v = nil
	}
	if v != nil {
		if !v.e.Alive() {
			embeds.drop(id)
			return EmbedState{"unavailable", "the VM's window is gone"}, nil
		}
		ox, oy, scale := webviewOrigin(win)
		v.e.SetRect(int16(ox*float64(scale)+x*dpr), int16(oy*float64(scale)+y*dpr), uint16(w*dpr), uint16(h*dpr))
		return EmbedState{State: "embedded"}, nil
	}
	if busy {
		return EmbedState{State: "waiting"}, nil
	}
	e, err := entry(id)
	if err != nil {
		return EmbedState{}, err
	}
	run, release, err := core.ClaimGUIRun(e)
	if err != nil {
		return EmbedState{"unavailable", err.Error()}, nil
	}
	if !run.Reims {
		release()
		return EmbedState{"unavailable", "not a Reims vGPU run"}, nil
	}
	embeds.mu.Lock()
	embeds.busy[id] = true
	embeds.mu.Unlock()
	go embeds.take(id, host, win, uint32(run.PID), release)
	return EmbedState{State: "waiting"}, nil
}

// take finds the run's Reims window and embeds it into win; the page's next EmbedScreen places it.
func (m *embedder) take(id, host string, win *application.WebviewWindow, pid uint32, release func()) {
	fail := func() {
		release()
		m.mu.Lock()
		delete(m.busy, id)
		m.mu.Unlock()
	}
	m.mu.Lock()
	if m.x == nil {
		x, err := embed.Open("")
		if err != nil {
			m.mu.Unlock()
			fail()
			return
		}
		m.x = x
	}
	x := m.x
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), findTimeout)
	defer cancel()
	child, err := x.FindWindow(ctx, pid, embed.ReimsTitle)
	if err != nil {
		fail()
		return
	}
	parent := windowXID(win)
	if parent == 0 {
		fail()
		return
	}
	e, err := x.Embed(xproto.Window(parent), child, pid, gtkSaveSet)
	if err != nil {
		fail()
		return
	}
	v := &vmEmbed{host: host, e: e, release: release, stop: make(chan struct{})}
	m.mu.Lock()
	m.vms[id] = v
	delete(m.busy, id)
	m.mu.Unlock()
	go func() { // keep it in place; a window that died or was taken away is let go
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-v.stop:
				return
			case <-t.C:
				v.e.Pump()
				if !v.e.Alive() {
					m.drop(id)
					return
				}
			}
		}
	}()
}

// EmbedVisible hides the VM's window while an HTML menu or dialog covers the screen area.
func (a *App) EmbedVisible(id string, visible bool) {
	embeds.mu.Lock()
	v := embeds.vms[id]
	embeds.mu.Unlock()
	if v == nil {
		return
	}
	if visible {
		v.e.Show()
	} else {
		v.e.Hide()
	}
}

// drop gives id's window back to the desktop as a window of its own; the run goes on.
func (m *embedder) drop(id string) {
	m.mu.Lock()
	v := m.vms[id]
	delete(m.vms, id)
	m.mu.Unlock()
	if v == nil {
		return
	}
	select {
	case <-v.stop:
	default:
		close(v.stop)
	}
	v.e.Release()
	v.release()
}

// releaseAll lets every embedded window go: before the app exits (signals included).
func (m *embedder) releaseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.vms))
	for id := range m.vms {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.drop(id)
	}
}
