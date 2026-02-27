// Package lib provides an embeddable boringstreamer HTTP audio streaming service.
package lib

import (
	"io"
	"path/filepath"
	"strings"
)

// AudioDecoder progressively decodes audio from an io.Reader without
// buffering the entire file in memory first.
//
// OpenDecode reads the file header and returns the native sample rate,
// channel count, a next() iterator, and any error.  Each call to next()
// returns the next chunk of raw interleaved signed 16-bit samples in the
// file's native rate and layout, or nil when the stream is exhausted.
// The caller is responsible for closing the underlying io.Reader.
type AudioDecoder interface {
	OpenDecode(r io.Reader) (sampleRate int, channels int, next func() []int16, err error)
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
