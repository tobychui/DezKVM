package kvmhid

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// CloseCodeTakenOver is the websocket close code sent to a client whose
// session was taken over by another browser (4000-4999 is for applications).
const CloseCodeTakenOver = 4001

// hidClient is one connected HID websocket.
type hidClient struct {
	id   string       // client ID chosen by the browser page (?client=)
	kick func(string) // disconnects the client with a takeover notice
}

// takeoverNotice is pushed to a client before it is disconnected.
type takeoverNotice struct {
	Type   string `json:"type"` // "takeover"
	Reason string `json:"reason"`
}

// KickOtherClients disconnects every HID websocket that does not belong to
// keepID, telling the page its session was taken over. Sockets without an ID
// (pages running an old cached frontend) are only kicked by a page that has
// an ID, never by another ID-less request, so they cannot disconnect
// themselves. It returns the client IDs that were disconnected.
func (c *Controller) KickOtherClients(keepID string, reason string) []string {
	c.clientsMu.Lock()
	var victims []*hidClient
	for cl := range c.clients {
		if cl.id != keepID {
			victims = append(victims, cl)
		}
	}
	c.clientsMu.Unlock()
	ids := make([]string, 0, len(victims))
	for _, cl := range victims {
		cl.kick(reason)
		ids = append(ids, cl.id)
	}
	return ids
}

func (c *Controller) addClient(cl *hidClient) {
	c.clientsMu.Lock()
	defer c.clientsMu.Unlock()
	if c.clients == nil {
		c.clients = make(map[*hidClient]struct{})
	}
	c.clients[cl] = struct{}{}
}

func (c *Controller) removeClient(cl *hidClient) {
	c.clientsMu.Lock()
	defer c.clientsMu.Unlock()
	delete(c.clients, cl)
}

// upgrader is used to upgrade HTTP connections to WebSocket connections
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// hidACKReply is the JSON structure sent back to the client when a command
// with a "rid" field has been processed.
type hidACKReply struct {
	Rid    string `json:"rid"`
	Status string `json:"status"` // "ok" or "error"
}

// HIDWebSocketHandler handles incoming WebSocket connections for HID commands
func (c *Controller) HIDWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Failed to upgrade to websocket:", err)
		return
	}
	defer conn.Close()

	// Synchronize websocket writes (consumer ACKs vs. takeover notices)
	var wsMu sync.Mutex

	// Register so another browser taking over the session can disconnect
	// this one. A kicked client's further input is ignored right away,
	// even before its socket finishes closing.
	var kicked atomic.Bool
	client := &hidClient{id: r.URL.Query().Get("client")}
	client.kick = func(reason string) {
		if kicked.Swap(true) {
			return
		}
		wsMu.Lock()
		_ = conn.WriteJSON(takeoverNotice{Type: "takeover", Reason: reason})
		wsMu.Unlock()
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(CloseCodeTakenOver, "session taken over"),
			time.Now().Add(time.Second))
		// Drop the connection even if the page never answers the close
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	}
	c.addClient(client)
	defer c.removeClient(client)

	// Bounded command queue:
	// When full, the oldest command is dropped so new events are not
	// blocked behind stale ones (e.g. when the remote machine reboots
	// and serial sends start timing out).
	const maxQueueSize = 10
	cmdQueue := make(chan *HIDCommand, maxQueueSize)
	done := make(chan struct{})

	// Consumer goroutine: sends commands to the HID serial device
	go func() {
		defer close(done)
		for cmd := range cmdQueue {
			_, err := c.ConstructAndSendCmd(cmd)

			// If the client requested an ACK (rid is set), send a reply
			if cmd.Rid != "" {
				status := "ok"
				if err != nil {
					status = "error"
				}
				ack := hidACKReply{Rid: cmd.Rid, Status: status}
				wsMu.Lock()
				writeErr := conn.WriteJSON(ack)
				wsMu.Unlock()
				if writeErr != nil {
					log.Println("Error writing ACK:", writeErr)
				}
			} else if err != nil {
				log.Println("Error sending HID command:", err)
			}
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if !strings.Contains(err.Error(), "close") {
				log.Println("Error reading message:", err)
			}
			break
		}

		if kicked.Load() {
			continue // taken over: wait for the close, ignore input
		}

		var hidCmd HIDCommand
		if err := json.Unmarshal(message, &hidCmd); err != nil {
			log.Println("Error parsing message:", err)
			continue
		}

		// Record activity for mouse jiggler idle detection
		c.RecordActivity()

		// Commands with rid must not be dropped — they expect an ACK.
		// Send them directly to the queue (blocking if full).
		if hidCmd.Rid != "" {
			cmdQueue <- &hidCmd
			continue
		}

		// Fire-and-forget: try to enqueue; if full, drop the oldest event first
		select {
		case cmdQueue <- &hidCmd:
		default:
			// Queue full — discard the oldest pending command
			<-cmdQueue
			cmdQueue <- &hidCmd
		}
	}

	// WebSocket closed — drain queue and wait for consumer to finish
	close(cmdQueue)
	<-done
}
