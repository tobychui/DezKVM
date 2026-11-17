package webrtcstream

/*
	audio.go - Opus audio for the WebRTC session

	PCM from the capture card's ALSA device (usbcapture StreamAudioTo) is
	piped into an ffmpeg child that encodes 20 ms Opus packets into an Ogg
	stream on stdout. The packets are taken out of the Ogg pages and written
	to an Opus track of the peer connection.

	The audio track is sent in its own MediaStream, so the browser does not
	lip-sync it against the video. Lip sync would hold the video back by the
	latency of the audio pipeline, and the KVM picture must stay as fast as
	possible.

	The ALSA device has a single consumer: the audio websocket and a WebRTC
	session carrying audio take the capture over from each other.
*/

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"imuslab.com/dezkvm/dezkvmd/mod/videnc"
)

// AudioSource is the capture-side contract for audio (implemented by
// usbcapture.Instance): it delivers interleaved signed 16-bit little-endian
// PCM to fn until the context is cancelled or another consumer takes over.
type AudioSource interface {
	StreamAudioTo(ctx context.Context, fn func(pcm []byte) error) error
	AudioFormat() (sampleRate int, channels int)
}

// ErrOpusUnavailable is reported when no ffmpeg with an Opus encoder is
// installed; the browser then falls back to the audio websocket.
var ErrOpusUnavailable = errors.New("ffmpeg with an Opus encoder is not installed")

// opusEncoder picks the ffmpeg Opus encoder: libopus, or ffmpeg's native
// (experimental) encoder when ffmpeg was built without libopus.
func opusEncoder() (string, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return "", ErrOpusUnavailable
	}
	if videnc.FFmpegHasEncoder("libopus") {
		return "libopus", nil
	}
	if videnc.FFmpegHasEncoder("opus") {
		return "opus", nil
	}
	return "", ErrOpusUnavailable
}

// AudioBitrateForQuality maps the browser's audio quality setting to an Opus
// bitrate in kbps.
func AudioBitrateForQuality(quality string) int {
	switch quality {
	case "low":
		return 32
	case "high":
		return 128
	default:
		return 64
	}
}

// opusArgs builds the ffmpeg arguments: raw PCM on stdin, Ogg Opus with one
// 20 ms packet per page on stdout, flushed per packet for low latency.
func opusArgs(encoder string, sampleRate, channels, bitrateKbps int) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		// Start encoding at once. (No "-fflags nobuffer" here: for raw PCM
		// it throws away the first packets read while probing.)
		"-probesize", "32",
		"-analyzeduration", "0",
		"-f", "s16le",
		"-ar", strconv.Itoa(sampleRate),
		"-ac", strconv.Itoa(channels),
		"-i", "pipe:0",
		"-c:a", encoder,
	}
	if encoder == "libopus" {
		// CELT only, 5 ms algorithmic delay instead of 26.5 ms
		args = append(args, "-application", "lowdelay", "-frame_duration", "20")
	} else {
		// The native encoder buffers up to 360 ms by default
		args = append(args, "-strict", "experimental", "-opus_delay", "20")
	}
	return append(args,
		"-b:a", fmt.Sprintf("%dk", bitrateKbps),
		"-ar", "48000",
		"-f", "ogg",
		"-page_duration", "20000",
		"-flush_packets", "1",
		"pipe:1",
	)
}

// newAudioTrack creates the Opus track, in its own stream (see above).
func newAudioTrack() (*webrtc.TrackLocalStaticSample, error) {
	return webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio", "dezkvm-audio",
	)
}

// startAudio starts capturing PCM from src, encoding it to Opus and writing
// it to the session's audio track until ctx is cancelled.
func (s *Session) startAudio(ctx context.Context, src AudioSource, bitrateKbps int) error {
	encoder, err := opusEncoder()
	if err != nil {
		return err
	}
	if bitrateKbps <= 0 {
		bitrateKbps = AudioBitrateForQuality("")
	}
	sampleRate, channels := src.AudioFormat()

	cmd := exec.Command("ffmpeg", opusArgs(encoder, sampleRate, channels, bitrateKbps)...)
	stderr := &tailBuffer{max: 2048}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Opus encoder: %w", err)
	}
	s.audioCmd = cmd
	s.AudioEncoder = encoder

	// PCM capture -> ffmpeg
	go func() {
		err := src.StreamAudioTo(ctx, func(pcm []byte) error {
			_, err := stdin.Write(pcm)
			return err
		})
		if err != nil && !s.isClosed() {
			// e.g. the audio websocket took the capture over; the video
			// keeps running.
			log.Printf("WebRTC audio capture ended: %v\n", err)
		}
		stdin.Close() // ffmpeg drains and exits
	}()

	// ffmpeg -> Opus track
	go func() {
		s.pumpOpus(stdout)
		cmd.Process.Kill()
		cmd.Wait()
		if msg := strings.TrimSpace(stderr.String()); msg != "" && !s.isClosed() {
			log.Printf("WebRTC Opus encoder: %s\n", msg)
		}
	}()

	log.Printf("WebRTC audio: %s %d kbps from %d Hz / %d ch capture\n", encoder, bitrateKbps, sampleRate, channels)
	return nil
}

// pumpOpus writes the Opus packets of the Ogg stream to the audio track.
func (s *Session) pumpOpus(stream io.Reader) {
	ogg := newOggPacketReader(stream)
	for {
		pkt, err := ogg.next()
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF && !s.isClosed() {
				log.Printf("WebRTC audio stream ended: %v\n", err)
			}
			return
		}
		// Skip the OpusHead / OpusTags header packets
		if len(pkt) >= 8 && (string(pkt[:8]) == "OpusHead" || string(pkt[:8]) == "OpusTags") {
			continue
		}
		duration := opusPacketDuration(pkt)
		if duration <= 0 {
			continue
		}
		if err := s.audioTrack.WriteSample(media.Sample{Data: pkt, Duration: duration}); err != nil {
			if !s.isClosed() {
				log.Printf("WebRTC audio track write failed: %v\n", err)
			}
			return
		}
	}
}

// opusPacketDuration returns the audio duration of an Opus packet from its
// TOC byte (RFC 6716 section 3.1), or 0 for a malformed packet.
func opusPacketDuration(pkt []byte) time.Duration {
	if len(pkt) == 0 {
		return 0
	}
	toc := pkt[0]
	config := toc >> 3
	var frame time.Duration
	switch {
	case config < 12: // SILK: 10, 20, 40, 60 ms
		frame = [4]time.Duration{10, 20, 40, 60}[config&3] * time.Millisecond
	case config < 16: // Hybrid: 10, 20 ms
		frame = [2]time.Duration{10, 20}[config&1] * time.Millisecond
	default: // CELT: 2.5, 5, 10, 20 ms
		frame = [4]time.Duration{2500, 5000, 10000, 20000}[config&3] * time.Microsecond
	}
	frames := 1
	switch toc & 3 {
	case 1, 2:
		frames = 2
	case 3:
		if len(pkt) < 2 {
			return 0
		}
		frames = int(pkt[1] & 0x3f)
	}
	return frame * time.Duration(frames)
}

// oggPacketReader splits an Ogg bitstream into packets, following the
// lacing values of each page (a packet may span pages, and a page may hold
// several packets).
type oggPacketReader struct {
	r       *bufio.Reader
	header  [27]byte
	lacing  []byte // lacing values of the current page not consumed yet
	partial []byte // packet being assembled
}

func newOggPacketReader(r io.Reader) *oggPacketReader {
	return &oggPacketReader{r: bufio.NewReader(r)}
}

func (o *oggPacketReader) next() ([]byte, error) {
	for {
		for len(o.lacing) > 0 {
			size := int(o.lacing[0])
			o.lacing = o.lacing[1:]
			start := len(o.partial)
			o.partial = append(o.partial, make([]byte, size)...)
			if _, err := io.ReadFull(o.r, o.partial[start:]); err != nil {
				return nil, err
			}
			if size < 255 { // a lacing value below 255 ends the packet
				pkt := o.partial
				o.partial = nil
				return pkt, nil
			}
		}

		// Next page
		if _, err := io.ReadFull(o.r, o.header[:]); err != nil {
			return nil, err
		}
		if string(o.header[:4]) != "OggS" {
			return nil, errors.New("ogg: lost page sync")
		}
		if o.header[5]&0x01 == 0 {
			o.partial = nil // not a continued page: drop any unfinished packet
		}
		o.lacing = make([]byte, int(o.header[26]))
		if _, err := io.ReadFull(o.r, o.lacing); err != nil {
			return nil, err
		}
	}
}

// tailBuffer keeps the last max bytes written to it (ffmpeg stderr).
type tailBuffer struct {
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }
