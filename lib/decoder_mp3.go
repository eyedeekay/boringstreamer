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

// OpenDecode opens an MP3 stream and returns the sample rate, channel count
// (always 2 for go-mp3), and an iterator that yields one read-buffer's worth
// of raw stereo 16-bit samples per call.  next() returns nil when the stream
// is exhausted.
//
// Peak memory: at most readBytes bytes per decode step instead of the whole
// file.
func (mp3Decoder) OpenDecode(r io.Reader) (int, int, func() []int16, error) {
	d, err := mp3.NewDecoder(r)
	if err != nil {
		return 0, 0, nil, err
	}

	// readBytes is one output frame of 8820 stereo pairs expressed as bytes.
	// go-mp3 always outputs 16-bit little-endian stereo PCM.
	const readBytes = 8820 * 2 * 2
	buf := make([]byte, readBytes)

	next := func() []int16 {
		n, err := io.ReadFull(d, buf)
		if n == 0 {
			return nil
		}
		samples := make([]int16, n/2)
		for i := range samples {
			samples[i] = int16(binary.LittleEndian.Uint16(buf[i*2:]))
		}
		// io.ErrUnexpectedEOF means partial last read — valid final chunk.
		// Any other non-nil error after a partial read: samples already contains
		// valid decoded data so return them rather than silently discarding the
		// last chunk.  This matches the ErrUnexpectedEOF handling above and
		// ensures no samples are lost when an unexpected IO error terminates
		// the stream (AUDIT: "MP3 Decoder Silently Drops Samples").
		if err != nil && err != io.ErrUnexpectedEOF {
			return samples
		}
		return samples
	}
	return d.SampleRate(), 2, next, nil
}
