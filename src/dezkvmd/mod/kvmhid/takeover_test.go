package kvmhid

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func dialHID(t *testing.T, srv *httptest.Server, clientID string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/?client=" + clientID
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// TestKickOtherClients: when page B takes over, page A gets a takeover
// notice and close code 4001, and B stays connected.
func TestKickOtherClients(t *testing.T) {
	c := &Controller{}
	srv := httptest.NewServer(http.HandlerFunc(c.HIDWebSocketHandler))
	defer srv.Close()

	a := dialHID(t, srv, "page-a")
	defer a.Close()
	b := dialHID(t, srv, "page-b")
	defer b.Close()

	// Wait until both are registered
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.clientsMu.Lock()
		n := len(c.clients)
		c.clientsMu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d clients registered", n)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if ids := c.KickOtherClients("page-b", "taken over in test"); len(ids) != 1 || ids[0] != "page-a" {
		t.Fatalf("kicked %v, want [page-a]", ids)
	}

	_ = a.SetReadDeadline(time.Now().Add(3 * time.Second))
	var notice takeoverNotice
	if err := a.ReadJSON(&notice); err != nil {
		t.Fatalf("page A got no takeover notice: %v", err)
	}
	if notice.Type != "takeover" || notice.Reason != "taken over in test" {
		t.Fatalf("unexpected notice %+v", notice)
	}
	_, _, err := a.ReadMessage()
	if !websocket.IsCloseError(err, CloseCodeTakenOver) {
		t.Fatalf("page A: want close code %d, got %v", CloseCodeTakenOver, err)
	}

	// Page B is untouched
	_ = b.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := b.ReadMessage(); err == nil || websocket.IsCloseError(err, CloseCodeTakenOver) {
		t.Fatalf("page B should still be connected and silent, got %v", err)
	}

	// A is unregistered once its handler returns
	deadline = time.Now().Add(3 * time.Second)
	for {
		c.clientsMu.Lock()
		n := len(c.clients)
		c.clientsMu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d clients still registered after the kick", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
