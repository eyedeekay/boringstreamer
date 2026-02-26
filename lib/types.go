// Type definitions for boringstreamer
package lib

// nullWriter acts like /dev/null, discarding all writes.
type nullWriter struct{}

func (nw nullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// streamFrame represents a single audio or video chunk to be broadcast.
type streamFrame []byte

// broadcastResult represents a client's event after attempting to broadcast a frame.
type broadcastResult struct {
	qid int
	err error
}

// fileEntry carries a file path and the HTTP Content-Type that should be used
// when streaming that file. ContentType is "audio/wav" for all audio files
// (they are normalised to PCM-WAV regardless of source format) and the native
// MIME type for video files.
type fileEntry struct {
	Path        string
	ContentType string
}
