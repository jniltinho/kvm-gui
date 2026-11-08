package gui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jniltinho/kvm-gui/internal/core"
)

// fakeScreen is a 4x2 display that records input and changes on demand.
type fakeScreen struct {
	mu      sync.Mutex
	events  []string
	update  func()
	dirty   image.Rectangle
	closed  chan struct{}
	runDone chan struct{}
}

func newFakeScreen() *fakeScreen {
	return &fakeScreen{closed: make(chan struct{}), runDone: make(chan struct{})}
}
func (f *fakeScreen) Run() error { <-f.runDone; return errors.New("run ended") }
func (f *fakeScreen) Close() error {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}
func (f *fakeScreen) Key(down bool, k core.KeyCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, keyEvent(down, k.Scancode))
	return nil
}
func (f *fakeScreen) Pointer(b uint8, x, y int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, pointerEvent(b))
	return nil
}
func (f *fakeScreen) TakeDirty() (image.Rectangle, []byte, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.dirty
	f.dirty = image.Rectangle{}
	return r, make([]byte, r.Dx()*r.Dy()*4), 4, 2
}
func (f *fakeScreen) SetOnUpdate(u func()) { f.mu.Lock(); f.update = u; f.mu.Unlock() }
func (f *fakeScreen) change(r image.Rectangle) {
	f.mu.Lock()
	f.dirty = f.dirty.Union(r)
	u := f.update
	f.mu.Unlock()
	if u != nil {
		u()
	}
}
func (f *fakeScreen) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.events, " ")
}

func keyEvent(down bool, sc uint32) string {
	if down {
		return "down:" + string(rune('0'+sc%10))
	}
	return "up:" + string(rune('0'+sc%10))
}
func pointerEvent(b uint8) string { return "ptr:" + string(rune('0'+b)) }

func testServer(t *testing.T, gen string) (*screenServer, *fakeScreen) {
	t.Helper()
	s, err := newScreenServer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f := newFakeScreen()
	s.findRun = func(vm string) (screenRun, bool) { return screenRun{gen, "/fake"}, vm == "vm1" }
	s.dial = func(string) (screenSource, error) { return f, nil }
	return s, f
}

func dial(t *testing.T, s *screenServer, origin string) (*websocket.Conn, error) {
	t.Helper()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	c, _, err := websocket.Dial(t.Context(), s.URL(), &websocket.DialOptions{HTTPHeader: h})
	return c, err
}

func auth(t *testing.T, c *websocket.Conn, token, gen string) {
	t.Helper()
	b, _ := json.Marshal(authMsg{Token: token, VM: "vm1", Generation: gen})
	if err := c.Write(t.Context(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// closedWith waits for the server to close the connection and returns its status.
func closedWith(t *testing.T, c *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

// Only the app's origin, only with this launch's token, only within 2 s, only for the run's
// current generation (task 6.1).
func TestScreenAuthentication(t *testing.T) {
	s, _ := testServer(t, "gen1")
	for _, origin := range []string{"", "null", "http://evil.example", "wails://localhost:1", "wails://evil"} {
		if c, err := dial(t, s, origin); err == nil {
			c.CloseNow()
			t.Errorf("origin %q was accepted", origin)
		}
	}
	c, err := dial(t, s, appOrigin)
	if err != nil {
		t.Fatal(err)
	}
	auth(t, c, strings.Repeat("0", 64), "gen1")
	if st := closedWith(t, c); st != websocket.StatusPolicyViolation {
		t.Fatalf("wrong token: %v", st)
	}
	c, _ = dial(t, s, appOrigin)
	start := time.Now()
	if st := closedWith(t, c); st != websocket.StatusPolicyViolation || time.Since(start) < authTimeout || time.Since(start) > 3*time.Second {
		t.Fatalf("stalled authentication: %v after %v", st, time.Since(start))
	}
	c, _ = dial(t, s, appOrigin)
	auth(t, c, s.token, "gen0") // the run restarted: an older generation
	if st := closedWith(t, c); st != websocket.StatusPolicyViolation {
		t.Fatalf("stale run: %v", st)
	}
	// The run restarts while the dial waits for its socket: the new run's screen is not given.
	var mu sync.Mutex
	gen := "gen1"
	s.findRun = func(vm string) (screenRun, bool) {
		mu.Lock()
		defer mu.Unlock()
		return screenRun{gen, "/fake"}, vm == "vm1"
	}
	f := newFakeScreen()
	s.dial = func(string) (screenSource, error) { mu.Lock(); gen = "gen2"; mu.Unlock(); return f, nil }
	c, _ = dial(t, s, appOrigin)
	auth(t, c, s.token, "gen1")
	if st := closedWith(t, c); st != websocket.StatusPolicyViolation {
		t.Fatalf("restart during the dial: %v", st)
	}
}

// A message over 64 KiB closes the connection; frames go one at a time; every key and button
// the viewer held is released when it disconnects; the run's end closes the viewer (task 6.1).
func TestScreenSession(t *testing.T) {
	s, f := testServer(t, "gen1")
	c, err := dial(t, s, appOrigin)
	if err != nil {
		t.Fatal(err)
	}
	auth(t, c, s.token, "gen1")
	time.Sleep(100 * time.Millisecond)
	f.change(image.Rect(0, 0, 2, 1))
	f.change(image.Rect(2, 1, 4, 2)) // coalesced with the first while it is in flight or not
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	typ, data, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || len(data) < 12 {
		t.Fatalf("frame: %v %v %d", typ, err, len(data))
	}
	f.change(image.Rect(0, 0, 1, 1))
	quick, qcancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	if _, _, err := c.Read(quick); err == nil {
		t.Fatal("a second frame came before the first was acknowledged")
	}
	qcancel()
	// The read timeout closed this connection (coder/websocket): use a fresh one for input.
	c, _ = dial(t, s, appOrigin)
	auth(t, c, s.token, "gen1")
	send := func(m clientMsg) {
		b, _ := json.Marshal(m)
		if err := c.Write(t.Context(), websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
	}
	send(clientMsg{T: "key", Down: true, Code: 30}) // evdev KEY_A
	send(clientMsg{T: "pointer", X: 1, Y: 1, B: 1})
	time.Sleep(100 * time.Millisecond)
	c.Close(websocket.StatusNormalClosure, "")
	time.Sleep(200 * time.Millisecond)
	if log := f.log(); !strings.Contains(log, "down:") || !strings.HasSuffix(log, "ptr:0") || strings.Count(log, "up:") < 1 {
		t.Fatalf("held key and button not released: %s", log)
	}
	// Oversized client message.
	c, _ = dial(t, s, appOrigin)
	auth(t, c, s.token, "gen1")
	c.Write(t.Context(), websocket.MessageText, []byte(`{"t":"key","pad":"`+strings.Repeat("x", 70<<10)+`"}`))
	if st := closedWith(t, c); st != websocket.StatusMessageTooBig && st != -1 {
		t.Fatalf("oversized message: %v", st)
	}
	// The run ends: the viewer is closed.
	c, _ = dial(t, s, appOrigin)
	auth(t, c, s.token, "gen1")
	time.Sleep(100 * time.Millisecond)
	close(f.runDone)
	if st := closedWith(t, c); st != websocket.StatusNormalClosure {
		t.Fatalf("run end: %v", st)
	}
}
