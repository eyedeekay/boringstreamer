// HTTP handler for streaming audio and video to browsers
package lib

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// broadcastTimeout is the maximum time allowed for io.Copy to deliver one audio
// frame to a single HTTP client. Slow clients that exceed this limit are
// disconnected. Declared as a package-level variable so tests can reduce it to
// milliseconds without modifying production behaviour.
var broadcastTimeout = 44 * time.Second

// streamHandler wraps a mux to serve HTTP requests for audio streaming.
type streamHandler struct {
	*mux
}

// ServeHTTP handles HTTP requests and streams audio or video to browsers.
// Audio is delivered as an open-ended WAV stream (Content-Type: audio/wav).
// Video files are piped through with their native Content-Type (video/mp4 etc.).
// The Content-Type is determined from the file currently being broadcast when
// the client connects.
func (sh streamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	frames := make(chan streamFrame)
	qid, br := sh.subscribe(frames)
	if qid < 0 {
		log.Printf("Error: new connection request denied, already serving %v connections. See -h for details.", sh.streamer.MaxConnections)
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	ct := sh.currentContentType()

	w.Header().Set("Date", now.Format(http.TimeFormat))
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Server", "BoringStreamer/4.0")

	// Audio streams require a WAV header so browsers can identify the format.
	// Video streams carry their own container framing; no preamble is needed.
	var err error
	if ct == "audio/wav" {
		_, err = io.Copy(w, bytes.NewReader(wavHeader(44100, 2, 16)))
		if err != nil {
			br <- broadcastResult{qid, err}
			return
		}
	}
	// broadcast stream to w
	// result is buffered (capacity 1) so that the io.Copy goroutine can always
	// send its outcome and exit, even if ServeHTTP has already returned due to
	// a timeout. An unbuffered channel here caused a goroutine leak: the
	// io.Copy goroutine would block on the send indefinitely after a timeout.
	result := make(chan error, 1)
	for {
		// Use comma-ok so that a closed channel (mux cleaned up a zombie
		// client) causes a clean exit rather than a silent nil-write loop.
		buf, ok := <-frames
		if !ok {
			return
		}

		// copyErr is local to the goroutine; it is never shared with the outer
		// function.  The previous implementation captured the outer `err` by
		// reference and wrote it under a sync.Mutex, but the outer goroutine
		// also wrote `err` in the timeout branch without holding that mutex,
		// producing a data race detected by -race.
		go func(r chan error, b []byte) {
			_, copyErr := io.Copy(w, bytes.NewReader(b))
			r <- copyErr
		}(result, buf)

		select {
		case err = <-result:
			if err != nil {
				break
			}
			br <- broadcastResult{qid, nil} // frame streamed, no error, send ack
		case <-time.After(broadcastTimeout): // it's an error if io.Copy() is not finished within broadcastTimeout, ServeHTTP should exit
			err = fmt.Errorf("timeout: %v", broadcastTimeout)
		}

		if err != nil {
			break
		}
	}
	br <- broadcastResult{qid, err} // error, send nack
}
