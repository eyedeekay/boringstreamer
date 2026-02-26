package lib

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestWavHeaderLength verifies the header is exactly 44 bytes.
func TestWavHeaderLength(t *testing.T) {
	h := wavHeader(44100, 2, 16)
	if len(h) != 44 {
		t.Fatalf("header length=%d, want 44", len(h))
	}
}

// TestWavHeaderMagicBytes verifies the RIFF, WAVE, fmt, and data markers.
func TestWavHeaderMagicBytes(t *testing.T) {
	h := wavHeader(44100, 2, 16)

	if !bytes.Equal(h[0:4], []byte("RIFF")) {
		t.Errorf("bytes 0-3 = %q, want RIFF", h[0:4])
	}
	if !bytes.Equal(h[8:12], []byte("WAVE")) {
		t.Errorf("bytes 8-11 = %q, want WAVE", h[8:12])
	}
	if !bytes.Equal(h[12:16], []byte("fmt ")) {
		t.Errorf("bytes 12-15 = %q, want \"fmt \"", h[12:16])
	}
	if !bytes.Equal(h[36:40], []byte("data")) {
		t.Errorf("bytes 36-39 = %q, want data", h[36:40])
	}
}

// TestWavHeaderOpenEnded verifies that both the RIFF and data chunk sizes are
// 0xFFFFFFFF to signal an unbounded stream.
func TestWavHeaderOpenEnded(t *testing.T) {
	h := wavHeader(44100, 2, 16)

	riffSize := binary.LittleEndian.Uint32(h[4:])
	if riffSize != 0xFFFFFFFF {
		t.Errorf("RIFF size=0x%08X, want 0xFFFFFFFF", riffSize)
	}
	dataSize := binary.LittleEndian.Uint32(h[40:])
	if dataSize != 0xFFFFFFFF {
		t.Errorf("data size=0x%08X, want 0xFFFFFFFF", dataSize)
	}
}

// TestWavHeaderFields validates the encoded fields for a standard 44100/stereo/16-bit config.
func TestWavHeaderFields(t *testing.T) {
	h := wavHeader(44100, 2, 16)

	fmtLen := binary.LittleEndian.Uint32(h[16:])
	if fmtLen != 16 {
		t.Errorf("fmt chunk length=%d, want 16", fmtLen)
	}
	audioFmt := binary.LittleEndian.Uint16(h[20:])
	if audioFmt != 1 {
		t.Errorf("audio format=%d, want 1 (PCM)", audioFmt)
	}
	channels := binary.LittleEndian.Uint16(h[22:])
	if channels != 2 {
		t.Errorf("channels=%d, want 2", channels)
	}
	sampleRate := binary.LittleEndian.Uint32(h[24:])
	if sampleRate != 44100 {
		t.Errorf("sample rate=%d, want 44100", sampleRate)
	}
	byteRate := binary.LittleEndian.Uint32(h[28:])
	if byteRate != 44100*2*2 {
		t.Errorf("byte rate=%d, want %d", byteRate, 44100*2*2)
	}
	blockAlign := binary.LittleEndian.Uint16(h[32:])
	if blockAlign != 4 {
		t.Errorf("block align=%d, want 4", blockAlign)
	}
	bitsPerSample := binary.LittleEndian.Uint16(h[34:])
	if bitsPerSample != 16 {
		t.Errorf("bits per sample=%d, want 16", bitsPerSample)
	}
}

// TestWavHeaderMono verifies that a mono 8-bit configuration encodes correctly.
func TestWavHeaderMono(t *testing.T) {
	h := wavHeader(22050, 1, 8)

	channels := binary.LittleEndian.Uint16(h[22:])
	if channels != 1 {
		t.Errorf("channels=%d, want 1", channels)
	}
	sampleRate := binary.LittleEndian.Uint32(h[24:])
	if sampleRate != 22050 {
		t.Errorf("sample rate=%d, want 22050", sampleRate)
	}
	bitsPerSample := binary.LittleEndian.Uint16(h[34:])
	if bitsPerSample != 8 {
		t.Errorf("bits per sample=%d, want 8", bitsPerSample)
	}
}
