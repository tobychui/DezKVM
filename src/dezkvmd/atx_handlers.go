package main

import (
	"net/http"
)

/*
	atx_handlers.go

	Top-level HTTP handler wrappers for ATX power control (front-panel power /
	reset button simulation via the CH552G auxiliary MCU).  These thin
	functions extract the instance UUID from the request and delegate to the
	corresponding DezkVM methods defined in mod/dezkvm/handlers.go.

	Routes are registered in register_ipkvm_apis (api.go).
*/

// handleATXState returns the remote computer's power / HDD LED states.
// GET only.
func handleATXState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleATXState(w, r, uuid)
}

// handleATXTrigger simulates pressing a front-panel button.
// POST only.  JSON body: {"action":"power_click"|"power_hold"|"reset_click"}
func handleATXTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uuid := r.PathValue("uuid")
	dezkvmManager.HandleATXTrigger(w, r, uuid)
}
