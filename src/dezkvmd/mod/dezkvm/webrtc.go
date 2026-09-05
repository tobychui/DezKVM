package dezkvm

/*
	webrtc.go

	HTTP handlers for the WebRTC video streaming mode. The browser posts an
	SDP offer together with the desired encoder settings; the server spins up
	a videnc encoder fed from the capture card's MJPEG frames (see
	mod/webrtcstream) and replies with the SDP answer.

	Only one WebRTC session runs per instance; a new offer replaces the
	previous session, mirroring the MJPEG stream takeover behaviour.
*/

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"imuslab.com/dezkvm/dezkvmd/mod/videnc"
	"imuslab.com/dezkvm/dezkvmd/mod/webrtcstream"
)

// closeWebRTCSession tears down the active WebRTC session of an instance,
// if any. Called on new offers, stream reconfiguration and instance stop.
func (i *UsbKvmDeviceInstance) closeWebRTCSession() {
	if i.webrtcSession != nil {
		i.webrtcSession.Close()
		i.webrtcSession = nil
	}
}

// HandleWebRTCOffer starts a WebRTC session for the instance.
// POST JSON body:
//
//	{
//	  "sdp": "<offer sdp>",
//	  "encoder": "auto|intel-vaapi|v4l2m2m|software",   (optional, falls back to preferences)
//	  "variant": "raspberrypi|orangepi",                (optional)
//	  "bitrate_kbps": 4000                              (optional)
//	}
//
// Response: {"sdp": "<answer sdp>", "encoder_name": "..."}
func (d *DezkVM) HandleWebRTCOffer(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	if targetInstance.usbCaptureDevice == nil || !targetInstance.usbCaptureDevice.Capturing {
		http.Error(w, "Video capture is not running", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		SDP         string `json:"sdp"`
		Encoder     string `json:"encoder"`
		Variant     string `json:"variant"`
		BitrateKbps int    `json:"bitrate_kbps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SDP == "" {
		http.Error(w, "Invalid request body: missing SDP offer", http.StatusBadRequest)
		return
	}

	// Fill unset fields from the stored preferences.
	prefs := targetInstance.Preferences
	if prefs == nil {
		prefs = DefaultPreferences()
	}
	if req.Encoder == "" {
		req.Encoder = prefs.VideoEncoder
	}
	if req.Variant == "" {
		req.Variant = prefs.VideoEncoderVariant
	}
	if req.BitrateKbps <= 0 {
		req.BitrateKbps = prefs.VideoBitrateKbps
	}

	backend, variant := videnc.ParseBackendString(req.Encoder)
	if req.Variant != "" {
		variant = videnc.Variant(req.Variant)
	}

	// Replace any previous session.
	targetInstance.closeWebRTCSession()

	session, answer, err := webrtcstream.StartSession(targetInstance.usbCaptureDevice, req.SDP, webrtcstream.Options{
		Backend:     backend,
		Variant:     variant,
		BitrateKbps: req.BitrateKbps,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, videnc.ErrBackendUnavailable) {
			status = http.StatusBadRequest
		}
		http.Error(w, "Failed to start WebRTC session: "+err.Error(), status)
		return
	}
	targetInstance.webrtcSession = session

	log.Printf("WebRTC session started for instance %s using %s\n", instanceUuid, session.EncoderName)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"sdp":          answer.SDP,
		"type":         answer.Type.String(),
		"encoder_name": session.EncoderName,
	})
}

// HandleWebRTCStop closes the active WebRTC session of the instance.
func (d *DezkVM) HandleWebRTCStop(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	targetInstance.closeWebRTCSession()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleWebRTCEncoders reports the encoder backends available on this host.
func HandleWebRTCEncoders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(videnc.DetectBackends())
}
