// Package lib provides an embeddable boringstreamer HTTP audio streaming service.
package lib

import (
	"io"
	"path/filepath"
	"strings"
)

// AudioDecoder decodes an audio file from r into raw PCM.
// It returns interleaved signed 16-bit samples, the native sample rate,
// the number of channels, and any error.
type AudioDecoder interface {
	Decode(r io.Reader) (samples []int16, sampleRate int, channels int, err error)
}

// audioFormat maps a lowercase file extension to a decoder.
type audioFormat struct {
	Ext     string // e.g. ".mp3"
	Decoder AudioDecoder
}

// RegisteredAudioFormats is the list of supported audio formats.
// Add new formats here by appending in an init() function; no other
// file needs to change.
var RegisteredAudioFormats []audioFormat

// decoderForFile returns the AudioDecoder for the given filename based on its
// extension, or nil if the extension is not registered.
func decoderForFile(name string) AudioDecoder {
	ext := strings.ToLower(filepath.Ext(name))
	for _, f := range RegisteredAudioFormats {
		if f.Ext == ext {
			return f.Decoder
		}
	}
	return nil
}
