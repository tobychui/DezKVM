package main

import (
	"net/http"

	"imuslab.com/dezkvm/dezkvmd/mod/dezkvm"
)

/*
	webrtc_handlers.go

	Top-level HTTP handler wrappers for the WebRTC video streaming mode.
	These thin functions extract the instance UUID from the request and
	delegate to the corresponding DezkVM methods defined in
	mod/dezkvm/webrtc.go.

	Routes are registered in register_ipkvm_apis (api.go).
*/

// handleWebRTCOffer accepts an SDP offer and returns the SDP answer.
// POST only.  JSON body: {"sdp":"...", "encoder":"...", "variant":"...", "bitrate_kbps":N}
func handleWebRTCOffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleWebRTCOffer(w, r, uuid)
}

// handleWebRTCStop closes the active WebRTC session.  POST only.
func handleWebRTCStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleWebRTCStop(w, r, uuid)
}

// handleWebRTCEncoders reports the video encoder backends available on this
// host.  GET only.
func handleWebRTCEncoders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dezkvm.HandleWebRTCEncoders(w, r)
}
