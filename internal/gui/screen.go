package gui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/jniltinho/kvm-gui/internal/core"
)

// The screen of a running VM reaches the page over a WebSocket on 127.0.0.1 (design D5): only
// the app's own origin may connect, the first message must carry this launch's 256-bit token and
// name the VM and its run generation within 2 s, client messages are at most 64 KiB, one frame is
// in flight per viewer, and every key and button a viewer pressed is released when it goes.

const (
	appOrigin    = "wails://localhost" // the origin Wails serves the page from on Linux (guard.go)
	authTimeout  = 2 * time.Second
	maxClientMsg = 64 << 10
)

// screenSource is a running VM's display: core.VNC, or a fake in tests.
type screenSource interface {
	Run() error
	Close() error
	Key(down bool, k core.KeyCode) error
	Pointer(buttons uint8, x, y int) error
	TakeDirty() (image.Rectangle, []byte, int, int)
	SetOnUpdate(func())
}

type vncSource struct{ *core.VNC }

func (v vncSource) SetOnUpdate(f func()) { v.OnUpdate = f }

// screenRun is what the server needs to know about a VM's run.
type screenRun struct {
	Generation, VNCSocket string
}

type screenServer struct {
	token string
	ln    net.Listener
	srv   *http.Server
	// findRun and dial are core.FindRun and core.DialVNC; tests replace them.
	findRun func(vmID string) (screenRun, bool)
	dial    func(sock string) (screenSource, error)

	mu      sync.Mutex
	viewers map[*viewer]struct{}
}

func newScreenServer() (*screenServer, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &screenServer{token: hex.EncodeToString(b), ln: ln, viewers: map[*viewer]struct{}{}, findRun: findRunScreen,
		dial: func(sock string) (screenSource, error) {
			v, err := core.DialVNC(sock)
			if err != nil {
				return nil, err
			}
			return vncSource{v}, nil
		}}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.handle), ReadHeaderTimeout: 5 * time.Second}
	go s.srv.Serve(ln)
	return s, nil
}

func findRunScreen(vmID string) (screenRun, bool) {
	vms, err := core.ListVMs()
	if err != nil {
		return screenRun{}, false
	}
	e, ok := core.FindVM(vms, vmID)
	if !ok {
		return screenRun{}, false
	}
	r, ok := core.FindRun(e)
	if !ok || r.VMID != e.ID || r.VNCSocket == "" {
		return screenRun{}, false
	}
	return screenRun{r.Generation, r.VNCSocket}, true
}

// URL is the WebSocket address the page connects to.
func (s *screenServer) URL() string { return "ws://" + s.ln.Addr().String() + "/screen" }

func (s *screenServer) Close() error {
	s.mu.Lock()
	for v := range s.viewers {
		v.conn.Close(websocket.StatusGoingAway, "the app is closing")
	}
	s.mu.Unlock()
	return s.srv.Close()
}

type authMsg struct {
	Token      string `json:"token"`
	VM         string `json:"vm"`
	Generation string `json:"generation"`
}

// clientMsg is the page's input: ack (the last frame is drawn), key (evdev code), pointer
// (absolute position, button mask) and wheel (dy < 0 up).
type clientMsg struct {
	T    string `json:"t"`
	Down bool   `json:"down"`
	Code int    `json:"code"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	B    uint8  `json:"b"`
	DY   int    `json:"dy"`
}

type viewer struct {
	conn    *websocket.Conn
	src     screenSource
	keys    map[core.KeyCode]bool
	buttons uint8
	x, y    int
}

func (s *screenServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/screen" {
		http.NotFound(w, r)
		return
	}
	// Only the app's page: a missing, "null" or other origin is refused before the upgrade.
	if r.Header.Get("Origin") != appOrigin {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true}) // the origin is checked above
	if err != nil {
		return
	}
	conn.SetReadLimit(maxClientMsg)
	// Closed with its status (not by cancelling the read, which drops the connection silently).
	timer := time.AfterFunc(authTimeout, func() { conn.Close(websocket.StatusPolicyViolation, "no authentication within 2 s") })
	_, data, err := conn.Read(r.Context())
	if !timer.Stop() || err != nil {
		conn.CloseNow()
		return
	}
	var a authMsg
	if json.Unmarshal(data, &a) != nil || subtle.ConstantTimeCompare([]byte(a.Token), []byte(s.token)) != 1 {
		conn.Close(websocket.StatusPolicyViolation, "not authenticated")
		return
	}
	run, ok := s.findRun(a.VM)
	if !ok || a.Generation == "" || subtle.ConstantTimeCompare([]byte(run.Generation), []byte(a.Generation)) != 1 {
		conn.Close(websocket.StatusPolicyViolation, "that run is not running")
		return
	}
	src, err := s.dial(run.VNCSocket)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "the VM's screen is not available")
		return
	}
	v := &viewer{conn: conn, src: src, keys: map[core.KeyCode]bool{}}
	s.mu.Lock()
	s.viewers[v] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.viewers, v)
		s.mu.Unlock()
		v.release()
		src.Close()
	}()
	s.serve(r.Context(), v)
}

// serve sends frames, one in flight, and applies input until either side closes; the VM's run
// ending closes the VNC connection and so the viewer.
func (s *screenServer) serve(parent context.Context, v *viewer) {
	ctx, cancel := context.WithCancel(parent)
	var reading sync.WaitGroup
	defer reading.Wait() // the input goroutine is done before release reads its state
	defer cancel()
	update := make(chan struct{}, 1)
	acked := make(chan struct{}, 1)
	acked <- struct{}{}
	v.src.SetOnUpdate(func() {
		select {
		case update <- struct{}{}:
		default: // coalesced: the next frame carries every change since
		}
	})
	go func() { v.src.Run(); cancel() }()
	reading.Add(1)
	go func() {
		defer reading.Done()
		defer cancel()
		for {
			// The request's context: a run's end closes the connection with a status instead.
			_, data, err := v.conn.Read(parent)
			if err != nil {
				return
			}
			var m clientMsg
			if json.Unmarshal(data, &m) != nil {
				return
			}
			if m.T == "ack" {
				select {
				case acked <- struct{}{}:
				default:
				}
				continue
			}
			if v.input(m) != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			v.conn.Close(websocket.StatusNormalClosure, "the run ended")
			return
		case <-update:
		}
		select {
		case <-ctx.Done():
			v.conn.Close(websocket.StatusNormalClosure, "the run ended")
			return
		case <-acked:
		}
		r, pix, w, h := v.src.TakeDirty()
		if r.Empty() {
			acked <- struct{}{}
			continue
		}
		if err := v.conn.Write(ctx, websocket.MessageBinary, frameMessage(r, pix, w, h)); err != nil {
			return
		}
	}
}

// frameMessage is a binary frame: x, y, w, h of the changed part and the frame's full size
// (uint16, little endian), then its RGBA pixels.
func frameMessage(r image.Rectangle, pix []byte, w, h int) []byte {
	b := make([]byte, 12, 12+len(pix))
	for i, n := range []int{r.Min.X, r.Min.Y, r.Dx(), r.Dy(), w, h} {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(n))
	}
	return append(b, pix...)
}

func (v *viewer) input(m clientMsg) error {
	switch m.T {
	case "key":
		k, ok := core.KeyForX11(m.Code + 8)
		if !ok {
			return nil
		}
		if m.Down {
			v.keys[k] = true
		} else {
			delete(v.keys, k)
		}
		return v.src.Key(m.Down, k)
	case "pointer":
		v.buttons, v.x, v.y = m.B&0x7, m.X, m.Y
		return v.src.Pointer(v.buttons, m.X, m.Y)
	case "wheel":
		b := uint8(1 << 3) // button 4: up
		if m.DY > 0 {
			b = 1 << 4
		}
		if err := v.src.Pointer(v.buttons|b, v.x, v.y); err != nil {
			return err
		}
		return v.src.Pointer(v.buttons, v.x, v.y)
	}
	return errors.New("unknown message")
}

// release lets go of every key and button the viewer still holds.
func (v *viewer) release() {
	for k := range v.keys {
		v.src.Key(false, k)
	}
	if v.buttons != 0 {
		v.src.Pointer(0, v.x, v.y)
	}
}

// ScreenInfo is where the page connects for a running VM's screen, and the run it binds to.
type ScreenInfo struct {
	URL        string `json:"url"`
	Token      string `json:"token"`
	Generation string `json:"generation"`
}

// Screen tells the page how to open id's screen: the server's address, this launch's token and
// the current run's generation. The token stays in the page; it is never logged.
func (a *App) Screen(id string) (ScreenInfo, error) {
	if a.screen == nil {
		return ScreenInfo{}, errors.New("the screen server is not running")
	}
	r, ok := a.screen.findRun(id)
	if !ok {
		return ScreenInfo{}, errors.New("the VM is not running with a screen of this app")
	}
	return ScreenInfo{a.screen.URL(), a.screen.token, r.Generation}, nil
}
