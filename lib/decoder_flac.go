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

// Decode decodes a FLAC stream into interleaved stereo int16 samples.
// FLAC stores samples as int32 per channel; this function scales them to
// 16-bit and interleaves across channels.
func (flacDecoder) Decode(r io.Reader) ([]int16, int, int, error) {
	stream, err := flac.New(r)
	if err != nil {
		return nil, 0, 0, err
	}
	defer stream.Close()

	sampleRate := int(stream.Info.SampleRate)
	channels := int(stream.Info.NChannels)
	bps := int(stream.Info.BitsPerSample)

	var samples []int16
	for {
		f, err := stream.ParseNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, 0, err
		}

		nSamples := len(f.Subframes[0].Samples)
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
				samples = append(samples, s16)
			}
		}
	}

	return samples, sampleRate, channels, nil
}
