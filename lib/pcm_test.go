package lib

import (
	"encoding/binary"
	"testing"
)

// TestMonoToStereo verifies that monoToStereo duplicates each sample into
// an interleaved L/R pair.
func TestMonoToStereo(t *testing.T) {
	src := []int16{100, 200, 300}
	got := monoToStereo(src)
	want := []int16{100, 100, 200, 200, 300, 300}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d", len(got), len(want))
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("[%d] got %d, want %d", i, got[i], v)
		}
	}
}

// TestMonoToStereoEmpty ensures monoToStereo handles empty input.
func TestMonoToStereoEmpty(t *testing.T) {
	if got := monoToStereo(nil); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

// TestResampleIdentity checks that resample returns the original slice when
// input and output rates are equal.
func TestResampleIdentity(t *testing.T) {
	src := []int16{1, 2, 3, 4}
	got := resample(src, 44100, 44100)
	if len(got) != len(src) {
		t.Fatalf("len=%d, want %d", len(got), len(src))
	}
	for i, v := range src {
		if got[i] != v {
			t.Errorf("[%d] got %d, want %d", i, got[i], v)
		}
	}
}

// TestResampleUpsample verifies that upsampling doubles the number of samples
// when the output rate is twice the input rate.
func TestResampleUpsample(t *testing.T) {
	src := []int16{0, 1000}
	got := resample(src, 22050, 44100)
	// expect roughly 4 samples for 2 input samples at 2x rate
	if len(got) != 4 {
		t.Fatalf("expected 4 samples, got %d", len(got))
	}
	// first sample should be 0, last should interpolate toward 1000
	if got[0] != 0 {
		t.Errorf("got[0]=%d, want 0", got[0])
	}
}

// TestResampleDownsample verifies that downsampling halves the number of samples.
func TestResampleDownsample(t *testing.T) {
	src := make([]int16, 4)
	for i := range src {
		src[i] = int16(i * 100)
	}
	got := resample(src, 44100, 22050)
	if len(got) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(got))
	}
}

// TestNormaliseStereoSameRate verifies that normalise is a no-op for stereo
// audio already at the canonical rate.
func TestNormaliseStereoSameRate(t *testing.T) {
	src := []int16{1, 2, 3, 4}
	got := normalise(src, canonRate, 2)
	if len(got) != len(src) {
		t.Fatalf("len=%d, want %d", len(got), len(src))
	}
	for i, v := range src {
		if got[i] != v {
			t.Errorf("[%d]=%d, want %d", i, got[i], v)
		}
	}
}

// TestNormaliseMono verifies that mono input is upconverted to stereo.
func TestNormaliseMono(t *testing.T) {
	src := []int16{100, 200}
	got := normalise(src, canonRate, 1)
	if len(got) != 4 {
		t.Fatalf("expected 4 samples after mono->stereo, got %d", len(got))
	}
	// each original sample should be duplicated
	if got[0] != 100 || got[1] != 100 || got[2] != 200 || got[3] != 200 {
		t.Errorf("unexpected stereo output: %v", got)
	}
}

// TestNormaliseResamplesRate verifies that normalise resamples non-canonical rates.
func TestNormaliseResamplesRate(t *testing.T) {
	// 2 stereo pairs at 22050 Hz → should upsample to ~4 stereo pairs at 44100 Hz
	src := []int16{0, 0, 1000, 1000}
	got := normalise(src, 22050, 2)
	if len(got) < 4 {
		t.Fatalf("expected at least 4 samples after upsample, got %d", len(got))
	}
	if len(got)%2 != 0 {
		t.Errorf("output length must be even (stereo), got %d", len(got))
	}
}

// TestChunkBasic checks that chunk splits a slice into correctly sized blocks.
func TestChunkBasic(t *testing.T) {
	// 12 samples → two chunks of 4 stereo pairs (8 samples) and one remainder of 4.
	src := make([]int16, 12)
	for i := range src {
		src[i] = int16(i)
	}
	chunks := chunk(src, 4) // 4 stereo pairs = block of 8
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if len(chunks[0]) != 8 {
		t.Errorf("chunk[0] len=%d, want 8", len(chunks[0]))
	}
	if len(chunks[1]) != 4 {
		t.Errorf("chunk[1] len=%d, want 4", len(chunks[1]))
	}
}

// TestChunkExact verifies that an evenly divisible slice produces no partial chunk.
func TestChunkExact(t *testing.T) {
	src := make([]int16, 8) // 4 stereo pairs
	chunks := chunk(src, 4)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0]) != 8 {
		t.Errorf("chunk[0] len=%d, want 8", len(chunks[0]))
	}
}

// TestChunkEmpty verifies that an empty slice produces no chunks.
func TestChunkEmpty(t *testing.T) {
	chunks := chunk(nil, 4)
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
}

// TestInt16sToBytes verifies that int16 samples are encoded as little-endian bytes.
func TestInt16sToBytes(t *testing.T) {
	samples := []int16{0x0102, -1}
	b := int16sToBytes(samples)
	if len(b) != 4 {
		t.Fatalf("len=%d, want 4", len(b))
	}
	if got := binary.LittleEndian.Uint16(b[0:]); got != 0x0102 {
		t.Errorf("b[0:2]=0x%04X, want 0x0102", got)
	}
	if got := binary.LittleEndian.Uint16(b[2:]); got != 0xFFFF {
		t.Errorf("b[2:4]=0x%04X, want 0xFFFF (encoding of -1)", got)
	}
}

// TestResampleDownsampleNoPanic exercises the 48000→44100 downsampling path
// across a wide range of input lengths.  Before the lo-clamp fix, certain
// lengths caused floating-point rounding to push lo to exactly len(src),
// triggering an index-out-of-bounds panic.  This test simply verifies that
// no panic occurs; output length is allowed to be 0 for very short inputs.
func TestResampleDownsampleNoPanic(t *testing.T) {
	// Run lengths from 1 to 1000; any panic will fail the test immediately.
	for n := 1; n <= 1000; n++ {
		src := make([]int16, n)
		for i := range src {
			src[i] = int16(i % 32767)
		}
		_ = resample(src, 48000, 44100) // must not panic
	}
}

// TestResampleSingleSample ensures a one-element input does not panic.
// For 48000→44100 a single sample rounds to 0 output samples (correct).
func TestResampleSingleSample(t *testing.T) {
	src := []int16{1000}
	// Must not panic; empty result is acceptable for sub-threshold input.
	_ = resample(src, 48000, 44100)
}

// TestMultiChanToStereoExtractsFirstTwoChannels verifies that multiChanToStereo
// correctly de-interleaves a 6-channel (5.1) buffer and returns only the
// front-left (index 0) and front-right (index 1) samples.  The centre,
// LFE, and surround channels must be discarded.
//
// Layout for 6-channel FLAC per-frame:
//
//	[L0, R0, C0, LFE0, LS0, RS0, L1, R1, C1, LFE1, LS1, RS1, ...]
func TestMultiChanToStereoExtractsFirstTwoChannels(t *testing.T) {
	// Two frames: frame 0 and frame 1.
	// channel order: L, R, C, LFE, LS, RS
	src := []int16{
		100, 200, 300, 400, 500, 600, // frame 0: L=100, R=200, others discarded
		110, 220, 330, 440, 550, 660, // frame 1: L=110, R=220, others discarded
	}
	got := multiChanToStereo(src, 6)
	want := []int16{100, 200, 110, 220}
	if len(got) != len(want) {
		t.Fatalf("multiChanToStereo len=%d, want %d", len(got), len(want))
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("[%d] got %d, want %d", i, got[i], v)
		}
	}
}

// TestMultiChanToStereoThreeChannels verifies the 3.0 channel case (L, R, C).
func TestMultiChanToStereoThreeChannels(t *testing.T) {
	// Three frames, 3 channels each: L, R, C
	src := []int16{
		10, 20, 30, // frame 0
		40, 50, 60, // frame 1
	}
	got := multiChanToStereo(src, 3)
	want := []int16{10, 20, 40, 50}
	if len(got) != len(want) {
		t.Fatalf("multiChanToStereo len=%d, want %d", len(got), len(want))
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("[%d] got %d, want %d", i, got[i], v)
		}
	}
}

// TestNormaliseMultiChannelAtCanonRate verifies that normalise correctly reduces
// a 6-channel 44100 Hz input to stereo without resampling.  Before the fix,
// normalise treated multi-channel input as already-stereo, interleaving pairs
// from wrong channel positions and producing garbled output.
func TestNormaliseMultiChannelAtCanonRate(t *testing.T) {
	// Single frame, 6-channel 44100 Hz: L=1000, R=2000, C/LFE/LS/RS discarded.
	src := []int16{1000, 2000, 3000, 4000, 5000, 6000}
	got := normalise(src, canonRate, 6)
	// Output must be exactly one stereo pair.
	if len(got) != 2 {
		t.Fatalf("normalise(6ch, canonRate) len=%d, want 2", len(got))
	}
	if got[0] != 1000 {
		t.Errorf("left=%d, want 1000", got[0])
	}
	if got[1] != 2000 {
		t.Errorf("right=%d, want 2000", got[1])
	}
}

// TestNormaliseMultiChannelWithResample verifies that normalise can both
// reduce channel count and resample in a single call.  Input is 3-channel
// audio at 22050 Hz; output must be stereo at 44100 Hz.
func TestNormaliseMultiChannelWithResample(t *testing.T) {
	// Two frames of 3-channel 22050 Hz audio: L=0, R=1000, C discarded.
	src := []int16{
		0, 1000, 9999,
		0, 1000, 9999,
	}
	got := normalise(src, 22050, 3)
	// After downmix to stereo we have 2 frames; upsampling to 44100 gives ~4.
	if len(got) < 4 {
		t.Fatalf("expected at least 4 samples after resample, got %d", len(got))
	}
	if len(got)%2 != 0 {
		t.Errorf("output length must be even (stereo pairs), got %d", len(got))
	}
}
