package main

import (
	"net/http"

	"imuslab.com/dezkvm/dezkvmd/mod/hostmgr"
)

/*
	maintenance_handlers.go

	APIs of the maintenance mode (shell landing page, no KVM session): live
	state of the dezkvmd host, its usage history and processes, and restart /
	shutdown of the host, which needs the admin password again.
*/

var hostManager *hostmgr.Manager

// register_maintenance_apis starts the host sampling and registers the
// maintenance mode APIs.
func register_maintenance_apis(mux *http.ServeMux) {
	hostManager = hostmgr.New(hostmgr.Options{
		ValidatePassword: authManager.ValidatePassword,
	})
	hostManager.Start()

	authManager.HandleFunc("/api/v1/host/summary", hostManager.HandleSummary, mux)
	authManager.HandleFunc("/api/v1/host/cpuinfo", hostManager.HandleCPUInfo, mux)
	authManager.HandleFunc("/api/v1/host/history", hostManager.HandleHistory, mux)
	authManager.HandleFunc("/api/v1/host/processes", hostManager.HandleProcesses, mux)
	authManager.HandleFunc("/api/v1/host/power", hostManager.HandlePower, mux)
}
