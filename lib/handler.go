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
	// Determine the content type BEFORE subscribing so the client entry in
	// m.clients carries the correct type.  currentContentType() polls briefly
	// for the first file's type so a video-first playlist does not cause a
	// spurious WAV header to be written (AUDIT issue #4).
	ct := sh.currentContentType()
	frames := make(chan streamFrame)
	qid, br := sh.subscribe(frames, ct)
	if qid < 0 {
		log.Printf("Error: new connection request denied, already serving %v connections. See -h for details.", sh.streamer.MaxConnections)
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

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
			// Remove this client directly from m.clients rather than sending
			// via br (m.result).  The result channel is only read by the
			// broadcast goroutine when it has dispatched a frame to at least
			// one matching client (sent > 0).  When the broadcast goroutine is
			// idle (empty directory, content-type mismatch) sent == 0 and a
			// channel send would block forever, leaking this goroutine.
			//
			// unsubscribe closes the frames channel.  Any in-flight dispatch
			// goroutine that panics on the closed send will report an error
			// result itself (see mux.go dispatch goroutine), ensuring the
			// broadcast goroutine's expected 'sent' count is satisfied.
			// See: AUDIT edge case bug #4.
			sh.unsubscribe(qid)
			return
		}
	}
	// broadcast stream to w
	// result is buffered (capacity 1) so that the io.Copy goroutine can always
	// send its outcome and exit, even if ServeHTTP has already returned due to
	// a timeout. An unbuffered channel here caused a goroutine leak: the
	// io.Copy goroutine would block on the send indefinitely after a timeout.
	result := make(chan error, 1)
	// Reuse a single timer per connection instead of allocating a new
	// time.After on every frame iteration.  At ~5 frames/s each connection
	// would otherwise create ~5 timers/s with a 44 s lifetime, holding
	// ~220 live timers per client.  One timer per connection eliminates that
	// GC pressure.  See: AUDIT performance issue #7.
	timer := time.NewTimer(broadcastTimeout)
	defer timer.Stop()
	for {
		// Use comma-ok so that a closed channel (mux cleaned up a zombie
		// client) causes a clean exit rather than a silent nil-write loop.
		frame, ok := <-frames
		if !ok {
			return
		}
		buf := frame.data

		// copyErr is local to the goroutine; it is never shared with the outer
		// function.  The previous implementation captured the outer `err` by
		// reference and wrote it under a sync.Mutex, but the outer goroutine
		// also wrote `err` in the timeout branch without holding that mutex,
		// producing a data race detected by -race.
		go func(r chan error, b []byte) {
			_, copyErr := io.Copy(w, bytes.NewReader(b))
			r <- copyErr
		}(result, buf)

		// Reset the timer for this frame's delivery window.  Stop+drain is
		// required before Reset to avoid a spurious fire on iteration n+1
		// caused by an unread tick from iteration n.
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(broadcastTimeout)

		select {
		case err = <-result:
			if err != nil {
				break
			}
			br <- broadcastResult{qid, nil} // frame streamed, no error, send ack
		case <-timer.C: // it's an error if io.Copy() is not finished within broadcastTimeout, ServeHTTP should exit
			err = fmt.Errorf("timeout: %v", broadcastTimeout)
		}

		if err != nil {
			break
		}
	}
	br <- broadcastResult{qid, err} // error, send nack
}
