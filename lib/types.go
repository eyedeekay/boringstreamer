// Type definitions for boringstreamer
package lib

// nullWriter acts like /dev/null, discarding all writes.
type nullWriter struct{}

func (nw nullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// streamFrame represents a single audio frame to be broadcast.
type streamFrame []byte

// broadcastResult represents a client's event after attempting to broadcast a frame.
type broadcastResult struct {
	qid int
	err error
}
