package webrtcstream

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"imuslab.com/dezkvm/dezkvmd/mod/videnc"
)

func TestOpusPacketDuration(t *testing.T) {
	cases := []struct {
		name string
		pkt  []byte
		want time.Duration
	}{
		{"CELT FB 20ms", []byte{31 << 3, 0}, 20 * time.Millisecond},
		{"CELT FB 2.5ms", []byte{28 << 3, 0}, 2500 * time.Microsecond},
		{"CELT 2 frames", []byte{31<<3 | 1, 0}, 40 * time.Millisecond},
		{"CELT 3 frames (code 3)", []byte{31<<3 | 3, 3}, 60 * time.Millisecond},
		{"SILK NB 60ms", []byte{3 << 3, 0}, 60 * time.Millisecond},
		{"Hybrid SWB 10ms", []byte{12 << 3, 0}, 10 * time.Millisecond},
		{"empty", nil, 0},
		{"code 3 without count", []byte{31<<3 | 3}, 0},
	}
	for _, c := range cases {
		if got := opusPacketDuration(c.pkt); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// oggPage builds one Ogg page (CRC left zero, the reader does not check it).
func oggPage(headerType byte, lacing []byte, data []byte) []byte {
	page := make([]byte, 27)
	copy(page, "OggS")
	page[5] = headerType
	page[26] = byte(len(lacing))
	page = append(page, lacing...)
	return append(page, data...)
}

func TestOggPacketReader(t *testing.T) {
	head := []byte("OpusHead-fake")
	a := bytes.Repeat([]byte{0xA}, 300) // lacing 255,45
	b := bytes.Repeat([]byte{0xB}, 10)  // lacing 10
	c := bytes.Repeat([]byte{0xC}, 600) // spans two pages: 255,255 | 90
	d := bytes.Repeat([]byte{0xD}, 255) // exact multiple of 255: 255,0

	var stream bytes.Buffer
	stream.Write(oggPage(0x02, []byte{byte(len(head))}, head))
	// two packets in one page
	stream.Write(oggPage(0, []byte{255, 45, 10}, append(append([]byte{}, a...), b...)))
	// packet c continued over the next page, then packet d
	stream.Write(oggPage(0, []byte{255, 255}, c[:510]))
	stream.Write(oggPage(0x01, []byte{90, 255, 0}, append(append([]byte{}, c[510:]...), d...)))

	r := newOggPacketReader(&stream)
	for i, want := range [][]byte{head, a, b, c, d} {
		got, err := r.next()
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("packet %d: got %d bytes, want %d", i, len(got), len(want))
		}
	}
	if _, err := r.next(); err != io.EOF {
		t.Fatalf("expected EOF after the last packet, got %v", err)
	}
}

// TestOpusEncodePipeline pushes one second of a 440 Hz tone through the
// ffmpeg arguments used for WebRTC audio and checks that it comes back as
// 20 ms Opus packets covering the whole second.
func TestOpusEncodePipeline(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	for _, encoder := range []string{"libopus", "opus"} {
		t.Run(encoder, func(t *testing.T) {
			if !videnc.FFmpegHasEncoder(encoder) {
				t.Skipf("ffmpeg has no %s encoder", encoder)
			}
			const sampleRate, channels = 48000, 2
			pcm := make([]byte, 0, sampleRate*channels*2)
			for n := 0; n < sampleRate; n++ {
				v := int16(8000 * math.Sin(2*math.Pi*440*float64(n)/sampleRate))
				for ch := 0; ch < channels; ch++ {
					pcm = binary.LittleEndian.AppendUint16(pcm, uint16(v))
				}
			}

			cmd := exec.Command("ffmpeg", opusArgs(encoder, sampleRate, channels, 64)...)
			cmd.Stdin = bytes.NewReader(pcm)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}

			r := newOggPacketReader(stdout)
			var total time.Duration
			audioPackets := 0
			for {
				pkt, err := r.next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("reading ogg: %v (ffmpeg: %s)", err, stderr.String())
				}
				if len(pkt) >= 8 && (string(pkt[:8]) == "OpusHead" || string(pkt[:8]) == "OpusTags") {
					continue
				}
				d := opusPacketDuration(pkt)
				if d != 20*time.Millisecond {
					t.Errorf("packet %d: duration %v, want 20ms", audioPackets, d)
				}
				total += d
				audioPackets++
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("ffmpeg failed: %v: %s", err, stderr.String())
			}
			// 1 s of input, plus the encoder's padding packets at the end
			if total < time.Second || total > 1100*time.Millisecond {
				t.Fatalf("decoded %v of audio in %d packets, want about 1s", total, audioPackets)
			}
		})
	}
}

// toneSource is a fake AudioSource producing a 440 Hz tone in real time.
type toneSource struct {
	stopped chan struct{}
}

func (s *toneSource) AudioFormat() (int, int) { return 48000, 2 }

func (s *toneSource) StreamAudioTo(ctx context.Context, fn func(pcm []byte) error) error {
	defer close(s.stopped)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		chunk := make([]byte, 0, 960*4)
		for i := 0; i < 960; i++ {
			v := int16(8000 * math.Sin(2*math.Pi*440*float64(n)/48000))
			n++
			chunk = binary.LittleEndian.AppendUint16(chunk, uint16(v))
			chunk = binary.LittleEndian.AppendUint16(chunk, uint16(v))
		}
		if err := fn(chunk); err != nil {
			return err
		}
	}
}

// idleFrames is a fake FrameSource that never delivers a frame.
type idleFrames struct{}

func (idleFrames) StreamFramesTo(ctx context.Context, fn func([]byte) error) error {
	<-ctx.Done()
	return nil
}
func (idleFrames) GetCaptureDims() (int, int, int) { return 640, 480, 10 }

// TestSessionSendsOpusAudio negotiates a session with an in-process pion
// peer acting as the browser and checks that Opus RTP arrives on its own
// stream, and that closing the session stops the audio capture.
func TestSessionSendsOpusAudio(t *testing.T) {
	if _, err := opusEncoder(); err != nil {
		t.Skip(err)
	}
	if !videnc.FFmpegHasEncoder("libx264") {
		t.Skip("libx264 not available for the video side")
	}

	browser, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	for _, kind := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
		if _, err := browser.AddTransceiverFromKind(kind, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			t.Fatal(err)
		}
	}

	type trackInfo struct {
		kind     webrtc.RTPCodecType
		mime     string
		streamID string
	}
	tracks := make(chan trackInfo, 2)
	audioPackets := make(chan struct{}, 1000)
	browser.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		tracks <- trackInfo{track.Kind(), track.Codec().MimeType, track.StreamID()}
		if track.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		for {
			if _, _, err := track.ReadRTP(); err != nil {
				return
			}
			audioPackets <- struct{}{}
		}
	})

	offer, err := browser.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(browser)
	if err := browser.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered

	audio := &toneSource{stopped: make(chan struct{})}
	session, answer, err := StartSession(idleFrames{}, browser.LocalDescription().SDP, Options{
		Backend:          videnc.BackendSoftware,
		BitrateKbps:      500,
		STUNServers:      []string{},
		Audio:            audio,
		AudioBitrateKbps: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !session.HasAudio {
		t.Fatalf("session has no audio: %s", session.AudioError)
	}
	if err := browser.SetRemoteDescription(*answer); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(15 * time.Second)
	received := 0
	for received < 25 {
		select {
		case <-audioPackets:
			received++
		case info := <-tracks:
			if info.kind == webrtc.RTPCodecTypeAudio {
				if !strings.EqualFold(info.mime, webrtc.MimeTypeOpus) {
					t.Errorf("audio codec %s, want Opus", info.mime)
				}
				if info.streamID != "dezkvm-audio" {
					t.Errorf("audio stream id %q, want its own stream", info.streamID)
				}
			}
		case <-deadline:
			session.Close()
			t.Fatalf("received only %d Opus packets", received)
		}
	}

	session.Close()
	select {
	case <-audio.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("audio capture still running after the session closed")
	}
}
