package dezkvm

/*
	session_owner.go

	Only one browser page controls an instance at a time. Every page sends a
	random client ID (generated per page load) with its video stream request
	(MJPEG ?client=, WebRTC offer "client_id"), its HID websocket and its
	audio websocket. Starting a video stream makes that page the owner: the
	capture layer hands the frames over (the previous MJPEG/WebRTC consumer
	stops), and the HID websockets of every other page are disconnected with a
	takeover notice so the old page shows the takeover screen and can no
	longer move the cursor or type.

	Pages that were taken over are remembered ("revoked"): their HID and audio
	reconnects and video requests are refused, so a click on the old page
	cannot steal the audio or the session back. "Take back control" reloads
	the page, which gets a new ID.

	The same page restarting its own stream (resolution change, switching
	between MJPEG and WebRTC) keeps its ID and is not kicked.

	Pages without an ID (a browser still running a cached copy of the old
	frontend) share the empty ID: they take over from pages that have an ID,
	but never disconnect each other or themselves, which is how dezkvmd
	behaved before session ownership existed.
*/

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"imuslab.com/dezkvm/dezkvmd/mod/kvmhid"
)

// revokedTTL is how long a taken over page ID is remembered.
const revokedTTL = 24 * time.Hour

// errSessionTakenOver is returned when a taken over page asks for the stream.
var errSessionTakenOver = errors.New("this page's session was taken over by another browser window or device; reload the page to take back control")

const takeoverReason = "The session was taken over from another browser window or device."

// clientIDFromRequest returns the ?client= ID of a request.
func clientIDFromRequest(r *http.Request) string {
	return r.URL.Query().Get("client")
}

// isRevoked reports whether clientID belongs to a page that was taken over.
func (i *UsbKvmDeviceInstance) isRevoked(clientID string) bool {
	if clientID == "" {
		return false
	}
	i.videoOwnerMu.Lock()
	defer i.videoOwnerMu.Unlock()
	_, ok := i.revokedClients[clientID]
	return ok
}

// revokeLocked remembers clientID as taken over. Caller holds videoOwnerMu.
func (i *UsbKvmDeviceInstance) revokeLocked(clientID string) {
	if clientID == "" {
		return
	}
	if i.revokedClients == nil {
		i.revokedClients = make(map[string]time.Time)
	}
	now := time.Now()
	for id, at := range i.revokedClients {
		if now.Sub(at) > revokedTTL {
			delete(i.revokedClients, id)
		}
	}
	i.revokedClients[clientID] = now
}

// claimVideo makes clientID the owner of the instance and disconnects the HID
// websockets of all other pages. It fails for pages that were taken over.
func (i *UsbKvmDeviceInstance) claimVideo(clientID string) error {
	if i.isRevoked(clientID) {
		return errSessionTakenOver
	}

	var kicked []string
	if i.usbKVMController != nil {
		kicked = i.usbKVMController.KickOtherClients(clientID, takeoverReason)
	}

	i.videoOwnerMu.Lock()
	previous := i.videoOwner
	i.videoOwner = clientID
	if previous != clientID {
		i.revokeLocked(previous)
	}
	for _, id := range kicked {
		i.revokeLocked(id)
	}
	i.videoOwnerMu.Unlock()

	if previous != clientID || len(kicked) > 0 {
		log.Printf("Instance %s: client %s took over the session from %s, disconnected %d other HID connection(s)\n",
			i.uuid, shortID(clientID), shortID(previous), len(kicked))
	}
	return nil
}

// rejectTakenOverWebsocket answers a websocket request from a taken over page
// with the same takeover notice and close code its HID socket got, so the
// page shows the takeover screen instead of retrying.
func rejectTakenOverWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := takeoverUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]string{"type": "takeover", "reason": takeoverReason})
	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(kvmhid.CloseCodeTakenOver, "session taken over"),
		time.Now().Add(time.Second))
}

var takeoverUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func shortID(id string) string {
	if id == "" {
		return "(no id)"
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
