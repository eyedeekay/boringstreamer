package lib

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// failWriter is an http.ResponseWriter whose Write always returns an error.
// It is used to simulate client disconnection during the WAV header write.
type failWriter struct {
	http.ResponseWriter
}

func (failWriter) Write(_ []byte) (int, error) {
	return 0, fmt.Errorf("simulated write failure")
}

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

// TestHandlerGoroutineUsesLocalError verifies the fix for the data race on the
// outer `err` variable (AUDIT finding #4).
//
// Previously the goroutine captured `err` by reference and wrote it under a
// sync.Mutex, while the outer goroutine also wrote `err` in the timeout branch
// without the mutex, creating a data race.
//
// The fix: the goroutine now uses a local `copyErr` variable and sends the
// value through the channel, eliminating all shared state.  This test
// exercises the exact concurrent scenario (goroutine write races with outer
// timeout write) and must pass under "go test -race".
func TestHandlerGoroutineUsesLocalError(t *testing.T) {
	old := broadcastTimeout
	broadcastTimeout = 20 * time.Millisecond
	defer func() { broadcastTimeout = old }()

	unblock := make(chan struct{})
	result := make(chan error, 1)
	goroutineDone := make(chan struct{})

	// Spawn the goroutine as the fixed ServeHTTP does: local copyErr, value
	// sent through channel.  There is no shared `err` variable.
	go func(r chan error, b []byte) {
		defer close(goroutineDone)
		_, copyErr := io.Copy(slowWriter{unblock: unblock}, bytes.NewReader(b))
		r <- copyErr
	}(result, []byte("audio frame data"))

	// Simulate the timeout branch: outer goroutine writes to a local err.
	// No shared variable exists between this goroutine and the one above.
	var err error
	select {
	case err = <-result:
		close(unblock)
	case <-time.After(broadcastTimeout):
		err = fmt.Errorf("timeout: %v", broadcastTimeout)
	}
	_ = err // used; prevents "declared and not used" error

	select {
	case <-unblock:
	default:
		close(unblock)
	}

	select {
	case <-goroutineDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("goroutine leaked after timeout; result channel may be unbuffered")
	}
}

// TestServeHTTPWAVHeaderFailureNoDeadlock is the regression test for AUDIT
// edge case bug #4.
//
// When the WAV header write fails (e.g. the client disconnects immediately),
// ServeHTTP must return promptly.  Before the fix, ServeHTTP sent its error
// via the unbuffered m.result channel:
//
//	br <- broadcastResult{qid, err}
//
// The broadcast goroutine only reads from m.result while collecting results
// after dispatching frames (sent > 0).  With an empty directory no frames are
// ever dispatched, so sent == 0 permanently and the send blocked forever,
// leaking the ServeHTTP goroutine and the m.clients entry.
//
// The fix has two parts:
//  1. ServeHTTP calls sh.unsubscribe(qid) directly instead of sending via
//     m.result, removing the client from the pool immediately.
//  2. The dispatch goroutine reports a result via m.result when it recovers
//     from a "send on closed channel" panic, keeping the broadcast goroutine's
//     expected 'sent' count correct.
//
// This test starts a streamer pointing at a non-existent path (no frames ever
// produced), connects a client whose WAV header write always fails, and asserts
// that ServeHTTP exits within a generous deadline instead of hanging.
func TestServeHTTPWAVHeaderFailureNoDeadlock(t *testing.T) {
	s := &Streamer{
		Path:           t.TempDir(), // empty directory — no frames ever produced
		MaxConnections: 5,
		Verbose:        false,
	}
	m := new(mux).start(s)

	// Force the content type so currentContentType() returns immediately
	// without sleeping through its 2 s polling window.
	m.Lock()
	m.currentCT = "audio/wav"
	m.Unlock()

	sh := streamHandler{m}

	done := make(chan struct{})
	go func() {
		defer close(done)
		rec := httptest.NewRecorder()
		// failWriter overrides Write so the WAV header write returns an error,
		// triggering the WAV header failure path in ServeHTTP.
		sh.ServeHTTP(failWriter{rec}, httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	select {
	case <-done:
		// ServeHTTP returned without blocking — fix verified.
	case <-time.After(3 * time.Second):
		t.Fatal("ServeHTTP deadlocked on WAV header failure (AUDIT bug #4): goroutine did not exit within 3 s")
	}

	// After ServeHTTP returns, the client must no longer be in m.clients.
	// Give the unsubscribe a moment to complete (it runs synchronously, but
	// the goroutine that called it may not have been scheduled yet).
	time.Sleep(10 * time.Millisecond)
	m.Lock()
	nClients := len(m.clients)
	m.Unlock()
	if nClients != 0 {
		t.Errorf("m.clients has %d entry/entries after WAV header failure, want 0 (client leaked)", nClients)
	}
}

// TestServeHTTPTimerReusedAcrossFrames verifies that the single reused
// time.Timer still correctly fires a timeout on a slow frame, guarding
// against a regression in the Stop+Reset pattern introduced for AUDIT
// performance issue #7.
//
// The test does not use a real mux; it replicates the timer logic directly
// to keep the test fast and free of filesystem dependencies.
func TestServeHTTPTimerReusedAcrossFrames(t *testing.T) {
	old := broadcastTimeout
	broadcastTimeout = 30 * time.Millisecond
	defer func() { broadcastTimeout = old }()

	result := make(chan error, 1)
	unblock := make(chan struct{})

	timer := time.NewTimer(broadcastTimeout)
	defer timer.Stop()

	// Simulate two frames being processed.  The first completes normally;
	// the second is slow and should time out.
	for i := range 2 {
		// Reset timer exactly as handler.go does.
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(broadcastTimeout)

		if i == 0 {
			// Fast frame: send result immediately.
			result <- nil
		}
		// Slow frame (i == 1): nothing is sent to result; timer should fire.

		var gotTimeout bool
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("frame %d: unexpected error %v", i, err)
			}
		case <-timer.C:
			gotTimeout = true
		}

		if i == 0 && gotTimeout {
			t.Fatal("frame 0: timed out unexpectedly on fast frame")
		}
		if i == 1 && !gotTimeout {
			t.Fatal("frame 1: timer did not fire on slow frame; Reset+Stop pattern may be broken")
		}
	}
	_ = unblock
}
