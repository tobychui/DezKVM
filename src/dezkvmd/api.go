package main

import (
	"log"
	"net/http"

	"imuslab.com/dezkvm/dezkvmd/mod/usbcapture"
	"imuslab.com/dezkvm/dezkvmd/mod/utils"
)

func register_auth_apis(mux *http.ServeMux) {
	// Check API for session validation
	mux.HandleFunc("/api/v1/check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ok := authManager.UserIsLoggedIn(r)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{\"status\":\"ok\"}"))
	})
	// Login API
	mux.HandleFunc("/api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		err := authManager.LoginUser(w, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{\"status\":\"success\"}"))
	})

	// Logout API
	mux.HandleFunc("/api/v1/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		err := authManager.LogoutUser(w, r)
		if err != nil {
			http.Error(w, "Logout failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{\"status\":\"logged out\"}"))
	})
}

func register_ipkvm_apis(mux *http.ServeMux) {
	authManager.HandleFunc("/api/v1/stream/{uuid}/video", func(w http.ResponseWriter, r *http.Request) {
		instanceUUID := r.PathValue("uuid")
		log.Println("Requested video stream for instance UUID:", instanceUUID)
		dezkvmManager.HandleVideoStreams(w, r, instanceUUID)
	}, mux)

	authManager.HandleFunc("/api/v1/stream/{uuid}/audio", func(w http.ResponseWriter, r *http.Request) {
		instanceUUID := r.PathValue("uuid")
		dezkvmManager.HandleAudioStreams(w, r, instanceUUID)
	}, mux)

	authManager.HandleFunc("/api/v1/hid/{uuid}/events", func(w http.ResponseWriter, r *http.Request) {
		instanceUUID := r.PathValue("uuid")
		dezkvmManager.HandleHIDEvents(w, r, instanceUUID)
	}, mux)

	authManager.HandleFunc("/api/v1/mass_storage/switch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		instanceUUID, err := utils.PostPara(r, "uuid")
		if err != nil {
			http.Error(w, "Missing or invalid uuid parameter", http.StatusBadRequest)
			return
		}
		side, err := utils.PostPara(r, "side")
		if err != nil {
			http.Error(w, "Missing or invalid side parameter", http.StatusBadRequest)
			return
		}
		switch side {
		case "kvm":
			dezkvmManager.HandleMassStorageSideSwitch(w, r, instanceUUID, true)
		case "remote":
			dezkvmManager.HandleMassStorageSideSwitch(w, r, instanceUUID, false)
		default:
			http.Error(w, "Invalid side parameter", http.StatusBadRequest)
		}
	}, mux)

	authManager.HandleFunc("/api/v1/instances", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			dezkvmManager.HandleListInstances(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}, mux)

	authManager.HandleFunc("/api/v1/resolutions/{uuid}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		instanceUUID := r.PathValue("uuid")
		dezkvmManager.HandleGetSupportedResolutions(w, r, instanceUUID)
	}, mux)

	authManager.HandleFunc("/api/v1/resolution/{uuid}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		instanceUUID := r.PathValue("uuid")
		dezkvmManager.HandleGetCurrentResolution(w, r, instanceUUID)
	}, mux)

	authManager.HandleFunc("/api/v1/resolution/change", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		instanceUUID, err := utils.PostPara(r, "uuid")
		if err != nil {
			http.Error(w, "Missing or invalid uuid parameter", http.StatusBadRequest)
			return
		}
		width, err := utils.PostInt(r, "width")
		if err != nil {
			http.Error(w, "Missing or invalid width parameter", http.StatusBadRequest)
			return
		}
		height, err := utils.PostInt(r, "height")
		if err != nil {
			http.Error(w, "Missing or invalid height parameter", http.StatusBadRequest)
			return
		}
		fps, err := utils.PostInt(r, "fps")
		if err != nil {
			http.Error(w, "Missing or invalid fps parameter", http.StatusBadRequest)
			return
		}

		newResolution := &usbcapture.CaptureResolution{
			Width:  width,
			Height: height,
			FPS:    fps,
		}
		dezkvmManager.HandleChangeResolution(w, r, instanceUUID, newResolution)
	}, mux)
}
