package lib

import (
	"path/filepath"
	"strings"
)

// videoFormat maps a file extension to its HTTP Content-Type.
type videoFormat struct {
	Ext         string // e.g. ".mp4"
	ContentType string // e.g. "video/mp4"
}

// RegisteredVideoFormats is the list of supported video pass-through formats.
// MP4 and WebM play inline in all major browsers. AVI and MKV will trigger a
// download rather than inline playback due to browser limitations.
var RegisteredVideoFormats = []videoFormat{
	{".mp4", "video/mp4"},
	{".webm", "video/webm"},
	{".avi", "video/x-msvideo"},
	{".mkv", "video/x-matroska"},
}

// contentTypeForVideoFile returns the Content-Type for the given filename if its
// extension is a registered video format. The second return value is false when
// the extension is not recognised.
func contentTypeForVideoFile(name string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(name))
	for _, vf := range RegisteredVideoFormats {
		if vf.Ext == ext {
			return vf.ContentType, true
		}
	}
	return "", false
}

// contentTypeForFile returns the streaming Content-Type for any supported file:
//   - audio files (any registered AudioDecoder extension) → "audio/wav"
//   - video files (any registered video extension) → their native content type
//   - unknown files → ""
func contentTypeForFile(name string) string {
	if decoderForFile(name) != nil {
		return "audio/wav"
	}
	if ct, ok := contentTypeForVideoFile(name); ok {
		return ct
	}
	return ""
}
