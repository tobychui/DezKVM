package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"imuslab.com/dezkvm/dezkvmd/mod/sshprox"
)

/*
	DezKVM Terminal

	The terminal allow users to access the console of the VM directly in the browser, without the need of any third party software like VNC viewer or SSH client. It is implemented using gotty, a web-based terminal emulator written in Go. The terminal is accessed through a websocket connection, which is proxied by the sshprox module to allow users to connect to their host
	system directly, just like what the PiKVM Terminal function does.
*/

type SSHConnectionRequest struct {
	IPAddr   string `json:"ipaddr"`
	Port     int    `json:"port"`
	Username string `json:"username"`
}

type SSHConnectionResponse struct {
	Error string `json:"error,omitempty"`
}

// handleCreateSSHSession creates a new SSH proxy session and returns the session token
func handleCreateSSHSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the request body
	var req SSHConnectionRequest
	err := r.ParseForm()
	if err != nil {
		responseError(w, "Failed to parse request", http.StatusBadRequest)
		return
	}

	// Get form values
	req.IPAddr = r.FormValue("ipaddr")
	portStr := r.FormValue("port")
	if portStr == "" {
		req.Port = 22
	} else {
		fmt.Sscanf(portStr, "%d", &req.Port)
	}
	req.Username = r.FormValue("username")

	// Validate inputs
	if req.IPAddr == "" {
		responseError(w, "IP address or domain is required", http.StatusBadRequest)
		return
	}

	if req.Port <= 0 || req.Port > 65535 {
		responseError(w, "Invalid port number", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		responseError(w, "Username is required", http.StatusBadRequest)
		return
	}

	// Check if SSH proxy is supported on this platform
	if !sshprox.IsWebSSHSupported() {
		responseError(w, "SSH terminal is not supported on this platform", http.StatusNotImplemented)
		return
	}

	// Validate username and remote address to prevent injection
	err = sshprox.ValidateUsernameAndRemoteAddr(req.Username, req.IPAddr)
	if err != nil {
		responseError(w, fmt.Sprintf("Validation failed: %s", err.Error()), http.StatusBadRequest)
		return
	}

	// Check if the target is connectable (optional, with timeout)
	if !sshprox.IsSSHConnectable(req.IPAddr, req.Port) {
		responseError(w, "Cannot connect to the specified SSH server", http.StatusBadRequest)
		return
	}

	// Create a new SSH proxy instance
	gottyBinaryDir := filepath.Join(CONFIG_PATH, "gotty")
	instance, err := sshproxManager.NewSSHProxy(gottyBinaryDir)
	if err != nil {
		responseError(w, fmt.Sprintf("Failed to create SSH proxy: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	// Get next available port for this instance
	nextPort := sshproxManager.GetNextPort()

	// Create connection with a slight delay to allow gotty to start
	err = instance.CreateNewConnection(nextPort, req.Username, req.IPAddr, req.Port)
	if err != nil {
		responseError(w, fmt.Sprintf("Failed to create connection: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	// Wait a bit for gotty to start
	time.Sleep(500 * time.Millisecond)

	// Return the session UUID
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(instance.UUID)
}

// handleSSHWebInterface serves the gotty web interface for a given session
func handleSSHWebInterface(w http.ResponseWriter, r *http.Request) {
	// Extract the instance UUID from the path
	// Expected path: /web.ssh/{uuid}/...
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/web.ssh/"), "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	instanceUUID := pathParts[0]

	// Strip the /web.ssh/{uuid} prefix from the URL path
	originalPath := r.URL.Path
	prefix := "/web.ssh/" + instanceUUID
	if strings.HasPrefix(originalPath, prefix) {
		// Remove the prefix, keeping everything after it
		r.URL.Path = strings.TrimPrefix(originalPath, prefix)
		// Ensure path starts with /
		if !strings.HasPrefix(r.URL.Path, "/") {
			r.URL.Path = "/" + r.URL.Path
		}
	}

	// Forward the request to the instance's gotty server
	sshproxManager.HandleHttpByInstanceId(instanceUUID, w, r)
}

// responseError sends an error response in JSON format
func responseError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := SSHConnectionResponse{
		Error: message,
	}
	json.NewEncoder(w).Encode(resp)
}
