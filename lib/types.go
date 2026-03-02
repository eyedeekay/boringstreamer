// Type definitions for boringstreamer
package lib

// nullWriter acts like /dev/null, discarding all writes.
type nullWriter struct{}

func (nw nullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// streamFrame represents a single audio or video chunk to be broadcast.
// Each frame carries its content type so the broadcast goroutine can route
// frames only to clients that subscribed for that content type.
type streamFrame struct {
	data        []byte
	contentType string
}

// clientEntry pairs a client's frame channel with the content type that client
// expects.  The broadcast goroutine skips sending a frame to a client whose
// expected content type does not match the frame's content type, preventing
// raw video bytes from being injected into an audio stream and vice versa.
type clientEntry struct {
	ch chan streamFrame
	ct string // expected content type for this connection
}

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
