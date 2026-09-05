package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	os_user "os/user"
	"strings"
)

// register_auth_apis registers authentication-related API endpoints
func register_auth_apis(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/check", handleCheck)
	mux.HandleFunc("/api/v1/login", handleLogin)
	mux.HandleFunc("/api/v1/logout", handleLogout)
}

// register_ipkvm_apis registers IP-KVM-related API endpoints
func register_ipkvm_apis(mux *http.ServeMux) {
	// USB HID APIs
	authManager.HandleFunc("/api/v1/hid/{uuid}/events", handleHIDEvents, mux)
	authManager.HandleFunc("/api/v1/mouse_jiggler/{uuid}", handleMouseJiggler, mux)

	// Streaming APIs
	authManager.HandleFunc("/api/v1/stream/{uuid}/video", handleVideoStream, mux)
	authManager.HandleFunc("/api/v1/stream/{uuid}/audio", handleAudioStream, mux)
	authManager.HandleFunc("/api/v1/resolution/{uuid}", handleGetCurrentResolution, mux)
	authManager.HandleFunc("/api/v1/resolution/change", handleChangeResolution, mux)
	authManager.HandleFunc("/api/v1/reconnect/{uuid}", handleReconnectCapture, mux)

	// WebRTC streaming APIs
	authManager.HandleFunc("/api/v1/webrtc/{uuid}/offer", handleWebRTCOffer, mux)
	authManager.HandleFunc("/api/v1/webrtc/{uuid}/stop", handleWebRTCStop, mux)
	authManager.HandleFunc("/api/v1/webrtc/encoders", handleWebRTCEncoders, mux)

	// Mass storage APIs
	authManager.HandleFunc("/api/v1/mass_storage/switch", handleMassStorageSwitch, mux)

	// ATX power control APIs
	authManager.HandleFunc("/api/v1/atx/{uuid}/state", handleATXState, mux)
	authManager.HandleFunc("/api/v1/atx/{uuid}/trigger", handleATXTrigger, mux)

	// Storage volume & file-manager APIs
	authManager.HandleFunc("/api/v1/storage/{uuid}/volume", handleStorageVolumeInfo, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/files", handleStorageFiles, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/upload", handleStorageUpload, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/rename", handleStorageRename, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/move", handleStorageMove, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/copy", handleStorageCopy, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/download", handleStorageDownload, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/thumb", handleStorageThumbnail, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/unmount", handleStorageUnmount, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/format/preview", handleStorageFormatPreview, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/format", handleStorageDiskFormat, mux)

	// Image write APIs (write an ISO/IMG onto the mass storage device)
	authManager.HandleFunc("/api/v1/storage/{uuid}/isowrite", handleISOWrite, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/isowrite/upload", handleISOWriteUpload, mux)
	authManager.HandleFunc("/api/v1/storage/{uuid}/isowrite/status", handleISOWriteStatus, mux)

	// ISO / disk-image library APIs
	authManager.HandleFunc("/api/v1/iso/list", handleISOList, mux)
	authManager.HandleFunc("/api/v1/iso/upload", handleISOUpload, mux)
	authManager.HandleFunc("/api/v1/iso/delete", handleISODelete, mux)
	authManager.HandleFunc("/api/v1/iso/rename", handleISORename, mux)
	authManager.HandleFunc("/api/v1/iso/download", handleISODownload, mux)

	// Settings APIs
	authManager.HandleFunc("/api/v1/resolutions/{uuid}", handleGetSupportedResolutions, mux)
	authManager.HandleFunc("/api/v1/preferences/{uuid}", handlePreferences, mux)

	// Others
	authManager.HandleFunc("/api/v1/instances", handleListInstances, mux)
	authManager.HandleFunc("/api/v1/screenshot/{uuid}", handleScreenshot, mux)
}

// register_terminal_apis registers terminal-related API endpoints
func register_terminal_apis(mux *http.ServeMux) {
	authManager.HandleFunc("/api/tools/webssh", handleCreateSSHSession, mux)
	authManager.HandleFunc("/web.ssh/", handleSSHWebInterface, mux)
	authManager.HandleFunc("/api/tools/whoami", handleWhoAmI, mux)
}

// handleWhoAmI returns the current Unix user and a list of all login-capable users on the system
func handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Get current user
	currentUser, err := os_user.Current()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Unable to fetch current user"}`))
		return
	}

	// Parse /etc/passwd to collect users with a valid login shell
	users := []string{}
	f, err := os.Open("/etc/passwd")
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "#") || line == "" {
				continue
			}
			fields := strings.Split(line, ":")
			if len(fields) < 7 {
				continue
			}
			shell := fields[6]
			// Include only users with a real login shell
			if shell == "/bin/false" || shell == "/usr/sbin/nologin" || shell == "/sbin/nologin" || shell == "" {
				continue
			}
			users = append(users, fields[0])
		}
	}

	type whoamiResponse struct {
		Current string   `json:"current"`
		Users   []string `json:"users"`
	}

	resp := whoamiResponse{
		Current: currentUser.Username,
		Users:   users,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
