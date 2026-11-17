package dezkvm

/*
	webrtc.go

	HTTP handlers for the WebRTC video streaming mode. The browser posts an
	SDP offer together with the desired encoder settings; the server spins up
	a videnc encoder fed from the capture card's MJPEG frames (see
	mod/webrtcstream) and replies with the SDP answer. When the browser asks
	for audio, the capture card's audio is sent as Opus on the same peer
	connection instead of over the audio websocket.

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
		i.webrtcClientID = ""
	}
}

// HandleWebRTCOffer starts a WebRTC session for the instance.
// POST JSON body:
//
//	{
//	  "sdp": "<offer sdp>",
//	  "encoder": "auto|intel-vaapi|rkmpp|v4l2m2m|software",   (optional, falls back to preferences)
//	  "variant": "raspberrypi|orangepi",                (optional)
//	  "bitrate_kbps": 4000,                             (optional)
//	  "client_id": "<random per page load>",            (optional, see session_owner.go)
//	  "audio": true,                                    (optional, send Opus audio too)
//	  "audio_quality": "low|standard|high"              (optional, Opus bitrate)
//	}
//
// Response: {"sdp": "<answer sdp>", "encoder_name": "...", "audio": true|false,
// "audio_error": "<why audio is not sent>"}
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
		SDP          string `json:"sdp"`
		Encoder      string `json:"encoder"`
		Variant      string `json:"variant"`
		BitrateKbps  int    `json:"bitrate_kbps"`
		ClientID     string `json:"client_id"`
		Audio        bool   `json:"audio"`
		AudioQuality string `json:"audio_quality"`
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

	// This page takes over the session, then replaces any previous stream.
	clientID := req.ClientID
	if err := targetInstance.claimVideo(clientID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	targetInstance.closeWebRTCSession()

	opts := webrtcstream.Options{
		Backend:     backend,
		Variant:     variant,
		BitrateKbps: req.BitrateKbps,
	}
	if req.Audio {
		opts.Audio = targetInstance.usbCaptureDevice
		opts.AudioBitrateKbps = webrtcstream.AudioBitrateForQuality(req.AudioQuality)
	}

	session, answer, err := webrtcstream.StartSession(targetInstance.usbCaptureDevice, req.SDP, opts)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, videnc.ErrBackendUnavailable) {
			status = http.StatusBadRequest
		}
		http.Error(w, "Failed to start WebRTC session: "+err.Error(), status)
		return
	}
	targetInstance.webrtcSession = session
	targetInstance.webrtcClientID = clientID

	audioInfo := "no audio"
	if session.HasAudio {
		audioInfo = "Opus audio via " + session.AudioEncoder
	}
	log.Printf("WebRTC session started for instance %s using %s, %s\n", instanceUuid, session.EncoderName, audioInfo)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sdp":          answer.SDP,
		"type":         answer.Type.String(),
		"encoder_name": session.EncoderName,
		"audio":        session.HasAudio,
		"audio_error":  session.AudioError,
	})
}

// HandleWebRTCStop closes the active WebRTC session of the instance. With
// ?client=, only a session owned by that page is closed: a page that was
// taken over must not end the new owner's stream when it switches modes or
// goes away.
func (d *DezkVM) HandleWebRTCStop(w http.ResponseWriter, r *http.Request, instanceUuid string) {
	targetInstance, err := d.GetInstanceByUUID(instanceUuid)
	if err != nil {
		http.Error(w, "Instance with specified UUID not found", http.StatusNotFound)
		return
	}
	if clientID := clientIDFromRequest(r); clientID != "" && clientID != targetInstance.webrtcClientID {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "not_owner"})
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
