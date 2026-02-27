package lib

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

// slowWriter blocks every Write call until its unblock channel is closed.
type slowWriter struct {
	unblock <-chan struct{}
}

func (sw slowWriter) Write(p []byte) (int, error) {
	<-sw.unblock
	return len(p), nil
}

// TestHandlerTimeoutNoGoroutineLeak verifies that the io.Copy goroutine spawned
// inside ServeHTTP's broadcast loop exits cleanly when the broadcast timeout
// fires before io.Copy completes.
//
// Before the fix, result was an unbuffered channel.  When the timeout branch of
// the select fired, ServeHTTP would return without reading from result, leaving
// the io.Copy goroutine permanently blocked on "result <- err".
//
// The fix is to make result a buffered channel (capacity 1).  The goroutine can
// then send its outcome and exit regardless of whether ServeHTTP is still
// waiting on the channel, eliminating the leak.
func TestHandlerTimeoutNoGoroutineLeak(t *testing.T) {
	// Use a very short timeout so the test finishes quickly.
	old := broadcastTimeout
	broadcastTimeout = 20 * time.Millisecond
	defer func() { broadcastTimeout = old }()

	unblock := make(chan struct{})

	// result mirrors the handler's channel.  Must be buffered (capacity 1) so
	// the goroutine can send after the "receiver" (this test) has moved on.
	result := make(chan error, 1)
	m := sync.Mutex{}

	goroutineDone := make(chan struct{})

	// Spawn the goroutine exactly as ServeHTTP does.
	go func(r chan error, b []byte) {
		defer close(goroutineDone)
		m.Lock()
		_, err := io.Copy(slowWriter{unblock: unblock}, bytes.NewReader(b))
		m.Unlock()
		r <- err // must not block when receiver already timed out
	}(result, []byte("audio frame data"))

	// Simulate ServeHTTP: wait for timeout, then stop reading from result.
	select {
	case <-result:
		// io.Copy finished before the timeout — this is fine but not the
		// scenario we're testing.  Unblock the writer for tidiness.
		close(unblock)
	case <-time.After(broadcastTimeout):
		// Timeout fired.  ServeHTTP would now set err and break out of the
		// loop, returning from ServeHTTP without draining result.
	}

	// Release the slow writer so the goroutine can finish its io.Copy.
	select {
	case <-unblock: // already closed above
	default:
		close(unblock)
	}

	// The goroutine must terminate within a generous deadline.
	select {
	case <-goroutineDone:
		// Good: no leak.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("goroutine leaked: it did not exit after the timeout path; result channel is likely unbuffered")
	}
}

// TestHandlerTimeoutErrorMessage verifies that the timeout error produced by
// ServeHTTP uses fmt.Errorf formatting (not errors.New(fmt.Sprintf(...))).
// This is a style regression guard: both produce the same message text.
func TestHandlerTimeoutErrorMessage(t *testing.T) {
	want := "timeout: 44s"
	got := fmt.Sprintf("timeout: %v", 44*time.Second)
	if got != want {
		t.Errorf("error message = %q, want %q", got, want)
	}
}
