package webrtcstream

/*
	webrtcstream - WebRTC H.264 video streaming for DezKVM

	Bridges the MJPEG frames captured from the HDMI capture card to a WebRTC
	peer connection: MJPEG frames are piped into a videnc encoder (hardware
	when available), and the resulting H.264 Annex-B stream is packetised
	into RTP by pion and delivered to the browser.

	Signaling is a single non-trickle offer/answer exchange over HTTPS
	(WHEP-style): the browser POSTs an SDP offer and receives the answer once
	ICE gathering completes. Only one WebRTC session runs per KVM instance;
	starting a new one replaces the previous session (same takeover
	behaviour as the MJPEG stream).

	Pure Go (pion/webrtc); the hardware encoders are reached through the
	videnc module.
*/

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/h264reader"

	"imuslab.com/dezkvm/dezkvmd/mod/videnc"
)

// FrameSource is the capture-side contract (implemented by
// usbcapture.Instance): it delivers MJPEG frames to fn until the context is
// cancelled or another consumer takes over the stream.
type FrameSource interface {
	StreamFramesTo(ctx context.Context, fn func(frame []byte) error) error
	GetCaptureDims() (width int, height int, fps int)
}

// Options configures one WebRTC streaming session.
type Options struct {
	Backend     videnc.Backend
	Variant     videnc.Variant
	BitrateKbps int
	// STUNServers used for ICE. Leave empty for host candidates only.
	STUNServers []string
}

// DefaultSTUNServers gives remote clients a chance to connect across NAT.
var DefaultSTUNServers = []string{"stun:stun.l.google.com:19302"}

// Session is one active WebRTC video streaming session.
type Session struct {
	pc         *webrtc.PeerConnection
	encoder    videnc.Encoder
	videoTrack *webrtc.TrackLocalStaticSample
	cancel     context.CancelFunc

	mu     sync.Mutex
	closed bool

	EncoderName string
}

// StartSession creates the encoder + peer connection, answers the given SDP
// offer and starts pumping frames. The returned answer must be delivered
// back to the browser.
func StartSession(src FrameSource, offerSDP string, opt Options) (*Session, *webrtc.SessionDescription, error) {
	width, height, fps := src.GetCaptureDims()
	if fps <= 0 {
		fps = 25
	}

	// --- Encoder -----------------------------------------------------
	enc, err := videnc.New(videnc.Config{
		Backend:     opt.Backend,
		Variant:     opt.Variant,
		Width:       width,
		Height:      height,
		FPS:         fps,
		BitrateKbps: opt.BitrateKbps,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := enc.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start encoder: %w", err)
	}

	// --- Peer connection ----------------------------------------------
	iceServers := []webrtc.ICEServer{}
	stuns := opt.STUNServers
	if stuns == nil {
		stuns = DefaultSTUNServers
	}
	for _, s := range stuns {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: []string{s}})
	}

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: iceServers})
	if err != nil {
		enc.Close()
		return nil, nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video", "dezkvm",
	)
	if err != nil {
		pc.Close()
		enc.Close()
		return nil, nil, err
	}
	if _, err := pc.AddTrack(track); err != nil {
		pc.Close()
		enc.Close()
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{
		pc:          pc,
		encoder:     enc,
		videoTrack:  track,
		cancel:      cancel,
		EncoderName: enc.Name(),
	}

	// Tear the whole session down when the browser goes away.
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("WebRTC ICE state: %s\n", state.String())
		switch state {
		case webrtc.ICEConnectionStateFailed,
			webrtc.ICEConnectionStateDisconnected,
			webrtc.ICEConnectionStateClosed:
			session.Close()
		}
	})

	// --- Encoded stream -> RTP track -----------------------------------
	go session.pumpEncodedStream(fps)

	// --- Capture frames -> encoder --------------------------------------
	// A tiny buffered channel decouples the capture rate from the encoder:
	// when the encoder cannot keep up, old frames are dropped instead of
	// building up latency.
	frameChan := make(chan []byte, 2)
	go func() {
		err := src.StreamFramesTo(ctx, func(frame []byte) error {
			// Copy: the capture buffer is reused by the v4l2 layer.
			buf := make([]byte, len(frame))
			copy(buf, frame)
			select {
			case frameChan <- buf:
			default:
				// Encoder busy — drop the oldest queued frame and retry.
				select {
				case <-frameChan:
				default:
				}
				select {
				case frameChan <- buf:
				default:
				}
			}
			return nil
		})
		if err != nil {
			log.Printf("WebRTC frame source ended: %v\n", err)
		}
		session.Close()
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-frameChan:
				if err := enc.WriteFrame(frame); err != nil {
					log.Printf("WebRTC encoder write failed: %v\n", err)
					session.Close()
					return
				}
			}
		}
	}()

	// --- Signaling -------------------------------------------------------
	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}
	if err := pc.SetRemoteDescription(offer); err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("invalid SDP offer: %w", err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		session.Close()
		return nil, nil, err
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		session.Close()
		return nil, nil, err
	}
	// Non-trickle: wait for ICE gathering so the answer carries all
	// candidates (bounded, STUN timeouts can be slow on isolated networks).
	select {
	case <-gatherComplete:
	case <-time.After(4 * time.Second):
		log.Println("WebRTC ICE gathering timed out; answering with partial candidates")
	}

	local := pc.LocalDescription()
	if local == nil {
		session.Close()
		return nil, nil, errors.New("no local SDP after ICE gathering")
	}
	return session, local, nil
}

// isVCLNal reports whether the NAL unit carries picture slice data.
func isVCLNal(t h264reader.NalUnitType) bool {
	return t >= h264reader.NalUnitTypeCodedSliceNonIdr && t <= h264reader.NalUnitTypeCodedSliceIdr
}

// isFirstSliceOfFrame reports whether a VCL NAL begins a new picture:
// first_mb_in_slice is the first ue(v) field of the slice header, and the
// value 0 is Exp-Golomb coded as a single '1' bit, i.e. the top bit of the
// byte after the NAL header is set.
func isFirstSliceOfFrame(nalData []byte) bool {
	return len(nalData) >= 2 && nalData[1]&0x80 != 0
}

// pumpEncodedStream reads H.264 NAL units from the encoder and writes them
// into the RTP track, aggregated into complete access units (one sample per
// video frame).
//
// Aggregation is essential: encoders may emit several slice NALs per frame
// (e.g. libx264's zerolatency tune uses sliced threads), and writing each
// NAL as its own timestamped sample makes the browser treat every slice as
// a separate frame — the picture decodes partially and then freezes.
func (s *Session) pumpEncodedStream(fps int) {
	reader, err := h264reader.NewReader(s.encoder.Output())
	if err != nil {
		log.Printf("WebRTC h264 reader init failed: %v\n", err)
		s.Close()
		return
	}

	frameDuration := time.Second / time.Duration(fps)
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	var accessUnit []byte // Annex-B concatenation of the current frame's NALs
	auHasSlice := false

	flush := func() bool {
		if len(accessUnit) == 0 {
			return true
		}
		writeErr := s.videoTrack.WriteSample(media.Sample{
			Data:     accessUnit,
			Duration: frameDuration,
		})
		accessUnit = nil
		auHasSlice = false
		if writeErr != nil && !s.isClosed() {
			log.Printf("WebRTC track write failed: %v\n", writeErr)
			s.Close()
			return false
		}
		return true
	}

	for {
		nal, err := reader.NextNAL()
		if err == io.EOF {
			flush()
			return
		}
		if err != nil {
			if !s.isClosed() {
				log.Printf("WebRTC h264 stream ended: %v\n", err)
				s.Close()
			}
			return
		}

		vcl := isVCLNal(nal.UnitType)

		// A new access unit starts when (a) a non-VCL NAL (SPS/PPS/SEI/AUD
		// of the next frame) follows slice data, or (b) a slice NAL with
		// first_mb_in_slice == 0 follows slice data of the previous frame.
		if auHasSlice && (!vcl || isFirstSliceOfFrame(nal.Data)) {
			if !flush() {
				return
			}
		}

		accessUnit = append(accessUnit, startCode...)
		accessUnit = append(accessUnit, nal.Data...)
		if vcl {
			auHasSlice = true
		}
	}
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Close stops the encoder, releases the capture stream and closes the peer
// connection. Safe to call multiple times.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	s.cancel()
	if s.encoder != nil {
		s.encoder.Close()
	}
	if s.pc != nil {
		s.pc.Close()
	}
	log.Println("WebRTC session closed")
}
