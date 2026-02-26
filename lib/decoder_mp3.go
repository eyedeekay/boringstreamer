package lib

// mp3Decoder implements AudioDecoder for MP3 files using
// github.com/hajimehoshi/go-mp3, which outputs interleaved stereo 16-bit
// little-endian PCM directly from an io.Reader.

import (
	"encoding/binary"
	"io"

	"github.com/hajimehoshi/go-mp3"
)

func init() {
	RegisteredAudioFormats = append(RegisteredAudioFormats,
		audioFormat{Ext: ".mp3", Decoder: mp3Decoder{}},
	)
}

type mp3Decoder struct{}

// Decode decodes an MP3 stream into interleaved stereo int16 samples.
// go-mp3 always produces stereo 16-bit little-endian PCM regardless of the
// source channel count.
func (mp3Decoder) Decode(r io.Reader) ([]int16, int, int, error) {
	d, err := mp3.NewDecoder(r)
	if err != nil {
		return nil, 0, 0, err
	}

	raw, err := io.ReadAll(d)
	if err != nil {
		return nil, 0, 0, err
	}

	// go-mp3 produces 16-bit little-endian interleaved stereo PCM.
	samples := make([]int16, len(raw)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
	}
	return samples, d.SampleRate(), 2, nil
}
