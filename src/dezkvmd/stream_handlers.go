package main

import (
	"log"
	"net/http"

	"imuslab.com/dezkvm/dezkvmd/mod/usbcapture"
	"imuslab.com/dezkvm/dezkvmd/mod/utils"
)

// handleVideoStream handles video streaming for a specific instance
func handleVideoStream(w http.ResponseWriter, r *http.Request) {
	instanceUUID := r.PathValue("uuid")
	log.Println("Requested video stream for instance UUID:", instanceUUID)
	dezkvmManager.HandleVideoStreams(w, r, instanceUUID)
}

// handleAudioStream handles audio streaming for a specific instance
func handleAudioStream(w http.ResponseWriter, r *http.Request) {
	instanceUUID := r.PathValue("uuid")
	dezkvmManager.HandleAudioStreams(w, r, instanceUUID)
}

// handleHIDEvents handles HID events for a specific instance
func handleHIDEvents(w http.ResponseWriter, r *http.Request) {
	instanceUUID := r.PathValue("uuid")
	dezkvmManager.HandleHIDEvents(w, r, instanceUUID)
}

// handleMassStorageSwitch switches mass storage between KVM and remote
func handleMassStorageSwitch(w http.ResponseWriter, r *http.Request) {
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
}

// handleListInstances lists all available instances
func handleListInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		dezkvmManager.HandleListInstances(w, r)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetSupportedResolutions returns supported resolutions for an instance
func handleGetSupportedResolutions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	instanceUUID := r.PathValue("uuid")
	dezkvmManager.HandleGetSupportedResolutions(w, r, instanceUUID)
}

// handleGetCurrentResolution returns the current resolution for an instance
func handleGetCurrentResolution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	instanceUUID := r.PathValue("uuid")
	dezkvmManager.HandleGetCurrentResolution(w, r, instanceUUID)
}

// handleChangeResolution changes the resolution for an instance
func handleChangeResolution(w http.ResponseWriter, r *http.Request) {
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
}

// handleScreenshot captures a single frame from the video device
func handleScreenshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	instanceUUID := r.PathValue("uuid")
	dezkvmManager.HandleScreenshot(w, r, instanceUUID)
}
