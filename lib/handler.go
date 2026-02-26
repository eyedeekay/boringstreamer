// HTTP handler for streaming audio to browsers
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

// ServeHTTP handles HTTP requests and streams WAV audio to browsers.
// Major browsers play a streaming WAV response natively when the data
// chunk size is set to 0xFFFFFFFF (open-ended).
func (sh streamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	frames := make(chan streamFrame)
	qid, br := sh.subscribe(frames)
	if qid < 0 {
		log.Printf("Error: new connection request denied, already serving %v connections. See -h for details.", sh.streamer.MaxConnections)
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	w.Header().Set("Date", now.Format(http.TimeFormat))
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Server", "BoringStreamer/4.0")

	// Write a single WAV header with an open-ended data chunk so browsers can
	// begin playback immediately and continue reading indefinitely.
	b := wavHeader(44100, 2, 16)
	_, err := io.Copy(w, bytes.NewReader(b))
	if err == nil {
		// broadcast mp3 stream to w
		broadcastTimeout := 44 * time.Second // timeout for slow clients
		result := make(chan error)
		m := sync.Mutex{}
		for {
			buf := <-frames

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
	}
	br <- broadcastResult{qid, err} // error, send nack
}
