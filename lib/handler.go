// HTTP handler for streaming audio and video to browsers
package lib

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

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
	broadcastTimeout := 44 * time.Second // timeout for slow clients
	result := make(chan error)
	m := sync.Mutex{}
	for {
		// Use comma-ok so that a closed channel (mux cleaned up a zombie
		// client) causes a clean exit rather than a silent nil-write loop.
		buf, ok := <-frames
		if !ok {
			return
		}

		go func(r chan error, b []byte) {
			m.Lock()
			_, err = io.Copy(w, bytes.NewReader(b))
			m.Unlock()
			r <- err
		}(result, buf)

		select {
		case err = <-result:
			if err != nil {
				break
			}
			br <- broadcastResult{qid, nil} // frame streamed, no error, send ack
		case <-time.After(broadcastTimeout): // it's an error if io.Copy() is not finished within broadcastTimeout, ServeHTTP should exit
			err = errors.New(fmt.Sprintf("timeout: %v", broadcastTimeout))
		}

		if err != nil {
			break
		}
	}
	br <- broadcastResult{qid, err} // error, send nack
}
