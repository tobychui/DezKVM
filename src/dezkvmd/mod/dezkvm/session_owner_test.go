package dezkvm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"imuslab.com/dezkvm/dezkvmd/mod/kvmhid"
)

func newTestInstance() (*DezkVM, *UsbKvmDeviceInstance) {
	inst := &UsbKvmDeviceInstance{uuid: "dev1", usbKVMController: &kvmhid.Controller{}}
	return &DezkVM{UsbKvmInstance: []*UsbKvmDeviceInstance{inst}}, inst
}

// hidServer serves the instance's HID websocket like the real route does.
func hidServer(d *DezkVM) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.HandleHIDEvents(w, r, "dev1")
	}))
}

// page is a test websocket client; closed receives its close code.
type page struct {
	conn   *websocket.Conn
	closed chan int
}

func dial(t *testing.T, srv *httptest.Server, clientID string) *page {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	if clientID != "" {
		url += "?client=" + clientID
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &page{conn: conn, closed: make(chan int, 1)}
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				code := -1
				if ce, ok := err.(*websocket.CloseError); ok {
					code = ce.Code
				}
				p.closed <- code
				return
			}
		}
	}()
	return p
}

func (p *page) Close() { p.conn.Close() }

// waitClosed returns the close code of p, or 0 if it stays open for d.
func waitClosed(p *page, d time.Duration) int {
	select {
	case code := <-p.closed:
		return code
	case <-time.After(d):
		return 0
	}
}

// A page that was taken over must not end the new owner's WebRTC session
// when it switches to MJPEG or goes away.
func TestWebRTCStopOnlyByOwner(t *testing.T) {
	d, inst := newTestInstance()
	inst.webrtcClientID = "new-owner"

	stop := func(client string) string {
		rec := httptest.NewRecorder()
		d.HandleWebRTCStop(rec, httptest.NewRequest("POST", "/api/v1/webrtc/dev1/stop?client="+client, nil), "dev1")
		var resp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp["status"]
	}
	if got := stop("old-page"); got != "not_owner" {
		t.Fatalf("stop from old page: status %q, want not_owner", got)
	}
	if inst.webrtcClientID != "new-owner" {
		t.Fatal("old page cleared the owner's session")
	}
	if got := stop("new-owner"); got != "ok" {
		t.Fatalf("stop from owner: status %q, want ok", got)
	}
}

// Takeover: page B claims, page A's HID is kicked and A can neither claim
// the video again nor reconnect HID.
func TestTakeoverRevokesOldPage(t *testing.T) {
	d, inst := newTestInstance()
	srv := hidServer(d)
	defer srv.Close()

	if err := inst.claimVideo("page-a"); err != nil {
		t.Fatal(err)
	}
	a := dial(t, srv, "page-a")
	defer a.Close()
	b := dial(t, srv, "page-b")
	defer b.Close()
	time.Sleep(100 * time.Millisecond)

	// Page A restarting its own stream keeps both sockets
	if err := inst.claimVideo("page-a"); err == nil {
		// page-b is kicked here because it has a different ID; reconnect it
	} else {
		t.Fatal(err)
	}
	if code := waitClosed(a, 300*time.Millisecond); code != 0 {
		t.Fatalf("page A was kicked by its own claim (code %d)", code)
	}

	b2 := dial(t, srv, "page-b2")
	defer b2.Close()
	time.Sleep(100 * time.Millisecond)
	if err := inst.claimVideo("page-b2"); err != nil {
		t.Fatal(err)
	}
	if code := waitClosed(a, 3*time.Second); code != kvmhid.CloseCodeTakenOver {
		t.Fatalf("page A: close code %d, want %d", code, kvmhid.CloseCodeTakenOver)
	}
	if code := waitClosed(b2, 300*time.Millisecond); code != 0 {
		t.Fatalf("new owner kicked (code %d)", code)
	}
	if err := inst.claimVideo("page-a"); err == nil {
		t.Fatal("taken over page A could claim the video again")
	}
	// A reconnecting its HID socket is refused with the takeover code
	a2 := dial(t, srv, "page-a")
	defer a2.Close()
	if code := waitClosed(a2, 3*time.Second); code != kvmhid.CloseCodeTakenOver {
		t.Fatalf("revoked page A HID reconnect: close code %d, want %d", code, kvmhid.CloseCodeTakenOver)
	}
}

// Pages running an old cached frontend send no client ID. Their stream
// restarts must not disconnect their own HID socket (the bug that made HID
// stop after any stream or resolution change).
func TestPagesWithoutIDDoNotKickThemselves(t *testing.T) {
	d, inst := newTestInstance()
	srv := hidServer(d)
	defer srv.Close()

	legacy := dial(t, srv, "")
	defer legacy.Close()
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 3; i++ {
		if err := inst.claimVideo(""); err != nil {
			t.Fatal(err)
		}
	}
	if code := waitClosed(legacy, 300*time.Millisecond); code != 0 {
		t.Fatalf("ID-less page kicked its own HID socket (code %d)", code)
	}
	// A page with an ID still takes over from it
	if err := inst.claimVideo("new-page"); err != nil {
		t.Fatal(err)
	}
	if code := waitClosed(legacy, 3*time.Second); code != kvmhid.CloseCodeTakenOver {
		t.Fatalf("ID-less page not taken over: close code %d", code)
	}
	// and the empty ID is never revoked
	if inst.isRevoked("") {
		t.Fatal("empty ID was revoked")
	}
}
