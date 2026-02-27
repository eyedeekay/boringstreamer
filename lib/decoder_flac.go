package lib

// flacDecoder implements AudioDecoder for FLAC files using
// github.com/mewkiz/flac, which provides frame-by-frame access to
// per-channel int32 samples at arbitrary bit depths.

import (
	"io"

	"github.com/mewkiz/flac"
)

func init() {
	RegisteredAudioFormats = append(RegisteredAudioFormats,
		audioFormat{Ext: ".flac", Decoder: flacDecoder{}},
	)
}

type flacDecoder struct{}

// OpenDecode opens a FLAC stream and returns the sample rate, channel count,
// and a frame-by-frame iterator.  Each next() call parses one FLAC audio
// frame and returns its interleaved int16 samples, scaled from the file's
// native bit depth; it returns nil at EOF or on error.
//
// Peak memory: one FLAC frame at a time (typically 4096 samples/channel)
// instead of the whole file.
func (flacDecoder) OpenDecode(r io.Reader) (int, int, func() []int16, error) {
	stream, err := flac.New(r)
	if err != nil {
		return 0, 0, nil, err
	}

	sampleRate := int(stream.Info.SampleRate)
	channels := int(stream.Info.NChannels)
	bps := int(stream.Info.BitsPerSample)

	next := func() []int16 {
		f, err := stream.ParseNext()
		if err != nil {
			// io.EOF is the normal end-of-stream signal.
			return nil
		}
		nSamples := len(f.Subframes[0].Samples)
		out := make([]int16, nSamples*channels)
		for i := 0; i < nSamples; i++ {
			for c := 0; c < channels; c++ {
				s32 := f.Subframes[c].Samples[i]
				var s16 int16
				switch {
				case bps > 16:
					s16 = int16(s32 >> (bps - 16))
				case bps < 16:
					s16 = int16(s32 << (16 - bps))
				default:
					s16 = int16(s32)
				}
				out[i*channels+c] = s16
			}
		}
		return out
	}
	return sampleRate, channels, next, nil
}
