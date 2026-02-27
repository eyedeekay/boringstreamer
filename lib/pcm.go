package lib

import "encoding/binary"

// canonRate is the canonical output sample rate for all streamed audio.
// All decoded audio is resampled to this rate before transmission.
const canonRate = 44100

// monoToStereo duplicates each mono sample into an interleaved L/R stereo pair.
func monoToStereo(src []int16) []int16 {
	out := make([]int16, len(src)*2)
	for i, s := range src {
		out[i*2] = s
		out[i*2+1] = s
	}
	return out
}

// resample converts a single-channel PCM signal from inRate to outRate using
// linear interpolation. Returns src unchanged when rates are equal.
func resample(src []int16, inRate, outRate int) []int16 {
	if inRate == outRate {
		return src
	}
	outLen := int(int64(len(src)) * int64(outRate) / int64(inRate))
	out := make([]int16, outLen)
	for i := range out {
		pos := float64(i) * float64(inRate) / float64(outRate)
		lo := int(pos)
		// Clamp lo: floating-point rounding during downsampling can push lo to
		// exactly len(src), which would panic on the src[lo] access below.
		if lo >= len(src) {
			lo = len(src) - 1
		}
		hi := lo + 1
		if hi >= len(src) {
			hi = len(src) - 1
		}
		frac := pos - float64(lo)
		out[i] = int16(float64(src[lo])*(1-frac) + float64(src[hi])*frac)
	}
	return out
}

// normalise converts arbitrary PCM (any sample rate, 1 or 2 channels) to the
// canonical stream format: 44100 Hz, stereo, interleaved int16.
func normalise(samples []int16, sampleRate, channels int) []int16 {
	if channels == 1 {
		samples = monoToStereo(samples)
	}
	if sampleRate == canonRate {
		return samples
	}
	// Split interleaved stereo channels, resample independently, re-interleave.
	n := len(samples) / 2
	left := make([]int16, n)
	right := make([]int16, n)
	for i := 0; i < n; i++ {
		left[i] = samples[i*2]
		right[i] = samples[i*2+1]
	}
	left = resample(left, sampleRate, canonRate)
	right = resample(right, sampleRate, canonRate)
	out := make([]int16, len(left)*2)
	for i := range left {
		out[i*2] = left[i]
		out[i*2+1] = right[i]
	}
	return out
}

// chunk splits a flat interleaved-stereo []int16 into sequential slices, each
// holding at most stereoSamples stereo pairs (2 int16 values per pair).
func chunk(samples []int16, stereoSamples int) [][]int16 {
	blockSize := stereoSamples * 2
	var out [][]int16
	for len(samples) >= blockSize {
		out = append(out, samples[:blockSize:blockSize])
		samples = samples[blockSize:]
	}
	if len(samples) > 0 {
		out = append(out, samples)
	}
	return out
}

// int16sToBytes encodes a slice of int16 samples as little-endian bytes.
// The returned slice has length len(samples)*2.
func int16sToBytes(samples []int16) []byte {
	b := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(s))
	}
	return b
}
