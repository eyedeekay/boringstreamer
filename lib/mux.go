// mux handles broadcasting audio streams to multiple subscribed clients.
package lib

import (
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// mux broadcasts audio or video stream to subscribed clients (ie. to http servers).
// Clients subscribe() and unsubscribe by writing to result channel.
type mux struct {
	sync.Mutex

	clients   map[int]clientEntry  // set of listener clients to be notified
	result    chan broadcastResult // clients share broadcast success-failure here
	currentCT string               // Content-Type of the currently streaming file
	streamer  *Streamer
}

// currentContentType returns the HTTP Content-Type of the file currently
// being streamed. It polls briefly for the first file's content type so that
// clients connecting before playback begins get the correct type rather than
// the wrong "audio/wav" default. This matters when the first shuffled file
// is a video: without the poll a connecting client would receive a stray WAV
// header followed by raw video container bytes.
//
// polling timeout: 20 × 100 ms = 2 s. In the normal case (files found quickly)
// the first content type is set within the 100 ms buffering window.
func (m *mux) currentContentType() string {
	const (
		pollInterval = 100 * time.Millisecond
		pollAttempts = 20
	)
	for i := 0; i < pollAttempts; i++ {
		m.Lock()
		ct := m.currentCT
		m.Unlock()
		if ct != "" {
			return ct
		}
		time.Sleep(pollInterval)
	}
	// Nothing has started within the polling window (e.g. empty directory).
	// Fall back to audio/wav so audio-only deployments remain unaffected.
	return "audio/wav"
}

// subscribe registers ch as a recipient of frames with the given content type.
// Only frames whose content type matches ct are delivered to ch, preventing
// raw video bytes from being injected into an audio stream and vice versa.
// Returns unique client id (qid) for ch and a broadcast result channel for the client.
// Returns -1, nil if too many clients are already listening.
func (m *mux) subscribe(ch chan streamFrame, ct string) (int, chan broadcastResult) {
	m.Lock()
	// Reject immediately when at or above capacity.  Using len(m.clients) makes
	// the capacity check O(1) and handles all boundary values of MaxConnections
	// (including 0) correctly and explicitly.
	if m.streamer.MaxConnections <= 0 || len(m.clients) >= m.streamer.MaxConnections {
		m.Unlock()
		return -1, nil
	}
	// Find the first unused qid by sequential scan.  In the common case where
	// clients are added and removed roughly in order this terminates quickly.
	qid := 0
	for {
		if _, occupied := m.clients[qid]; !occupied {
			break
		}
		qid++
	}
	m.clients[qid] = clientEntry{ch: ch, ct: ct}
	// Capture the current connection count while still holding the lock.
	// Reading len(m.clients) after Unlock would race with the broadcast
	// goroutine's lock-protected delete, causing a runtime panic under -race.
	nClients := len(m.clients)
	m.Unlock()
	if m.streamer.Verbose {
		fmt.Printf("New connection (qid: %v), streaming to %v connections, at %v\n", qid, nClients, time.Now().Format(time.Stamp))
	}

	return qid, m.result
}

// unsubscribe removes the client identified by qid from the broadcast pool and
// closes its frame channel.  It is safe to call from any goroutine and is
// idempotent: a second call for the same qid is a no-op.
//
// ServeHTTP calls this directly when the WAV header write fails, BEFORE
// entering the frame-receive loop.  Direct removal avoids DeadLock: without
// this method ServeHTTP would send an error via m.result, which only works
// when the broadcast goroutine is actively collecting results (sent > 0).  In
// the empty-directory or content-type-mismatch case sent == 0 and the channel
// send would block forever.  See: AUDIT edge case bug #4.
func (m *mux) unsubscribe(qid int) {
	m.Lock()
	defer m.Unlock()
	e, ok := m.clients[qid]
	if !ok {
		return
	}
	close(e.ch)
	delete(m.clients, qid)
}

// start initializes a multiplexer for raw audio streams using the provided Streamer config.
// e.g: m := new(mux).start(s)
func (m *mux) start(s *Streamer) *mux {
	m.streamer = s
	path := s.Path

	// Buffer capacity = MaxConnections so that ServeHTTP goroutines can
	// always send a broadcastResult (success or error) without blocking, even
	// when the broadcast goroutine is idle between frames or handling a
	// content-type that does not match the failing client.  Without a buffer,
	// the WAV header failure path in ServeHTTP deadlocks permanently.
	// See: AUDIT issue #4.
	resultBuf := s.MaxConnections
	if resultBuf < 1 {
		resultBuf = 1
	}
	m.result = make(chan broadcastResult, resultBuf)
	m.clients = make(map[int]clientEntry)

	// flow structure: fs -> nextFile -> nextFrame -> subscribed http servers -> browsers
	nextFile := make(chan fileEntry)    // next file to be broadcast (with its Content-Type)
	nextFrame := make(chan streamFrame) // next audio or video chunk

	// generate randomized list of files available from path
	// rand.Seed is omitted: Go 1.20+ automatically seeds the global source from
	// OS entropy, making an explicit call a no-op and a go vet warning.
	rescan := make(chan chan string)
	go func() {
		if path == "-" {
			return
		}

		for {
			files := <-rescan

			t0 := time.Now()
			notified := false
			// WalkDir is preferred over the deprecated filepath.Walk: it avoids a
			// redundant os.Lstat call per directory entry and is measurably faster
			// on large trees (available since Go 1.16).
			filepath.WalkDir(path, func(wpath string, d fs.DirEntry, err error) error {
				// notify user if no audio files are found after 4 seconds of walking
				dt := time.Now().Sub(t0)
				if dt > 4*time.Second && !notified && s.Verbose {
					fmt.Printf("Still looking for first audio file under %#v to broadcast, after %v... Maybe try -h flag.\n", path, dt)
					notified = true
				}

				if err != nil {
					return nil
				}
				// Honour -r=false: skip sub-directories that are not the root path.
				if d.IsDir() {
					if !s.Recursive && wpath != path {
						return filepath.SkipDir
					}
					return nil
				}
				if !d.Type().IsRegular() {
					return nil
				}
				// skip files with no registered handler (audio or video)
				if contentTypeForFile(d.Name()) == "" {
					return nil
				}

				files <- wpath // found file

				return nil
			})
			close(files)
			time.Sleep(1 * time.Second) // if no files are found, poll at least with 1Hz
		}
	}()

	// buffer and shuffle
	//
	// Strategy: create a single 100 ms deadline before the walk begins.
	// Files received before the deadline are buffered; once the deadline fires
	// we flush the buffer in shuffled order and stream all subsequent files
	// directly so playback starts as soon as possible on large libraries.
	// Using a single pre-created timer (rather than time.After inside the loop)
	// is the critical fix: a per-iteration time.After always produces a fresh,
	// unready channel so the default case fires every time, making the early-
	// start path permanently dead.
	//
	// The shuffle uses rand.Shuffle (Fisher-Yates) to produce a uniform random
	// permutation. The previous insert-at-random-index algorithm could produce
	// at most 2^(n-1) distinct orderings for n files instead of the required n!.
	go func() {
		if path == "-" {
			return
		}

		for {
			files := make(chan string)
			rescan <- files

			var buffered []string
			earlyDeadline := time.After(100 * time.Millisecond)
			streaming := false // true once the early deadline has fired

			for f := range files {
				if streaming {
					// Deadline already passed: forward directly for immediate playback.
					nextFile <- fileEntry{Path: f, ContentType: contentTypeForFile(f)}
					if s.Verbose {
						fmt.Printf("Next: %v\n", f)
					}
					continue
				}

				// Check whether the 100 ms buffering window has elapsed.
				select {
				case <-earlyDeadline:
					// Deadline just fired: shuffle and flush the buffer so
					// playback can begin before the entire walk completes.
					streaming = true
					rand.Shuffle(len(buffered), func(i, j int) {
						buffered[i], buffered[j] = buffered[j], buffered[i]
					})
					for _, sf := range buffered {
						nextFile <- fileEntry{Path: sf, ContentType: contentTypeForFile(sf)}
						if s.Verbose {
							fmt.Printf("Next: %v\n", sf)
						}
					}
					buffered = nil
					// Forward the file that triggered the deadline too.
					nextFile <- fileEntry{Path: f, ContentType: contentTypeForFile(f)}
					if s.Verbose {
						fmt.Printf("Next: %v\n", f)
					}
				default:
					// Still within the buffering window; accumulate for shuffle.
					buffered = append(buffered, f)
				}
			}

			// Walk complete. If the deadline never fired (fast walk or small
			// library), shuffle and queue the full buffer now.
			if !streaming {
				rand.Shuffle(len(buffered), func(i, j int) {
					buffered[i], buffered[j] = buffered[j], buffered[i]
				})
				for _, f := range buffered {
					nextFile <- fileEntry{Path: f, ContentType: contentTypeForFile(f)}
					if s.Verbose {
						fmt.Printf("Next: %v\n", f)
					}
				}
			}
		}
	}()

	// streamVideoFile pipes a video file directly to nextFrame in 64 KiB chunks,
	// tagging each chunk with ct so the broadcast goroutine only delivers it to
	// clients that subscribed for that video content type.
	// No timing is applied; the browser's media pipeline handles buffering.
	streamVideoFile := func(filename string, ct string) {
		f, err := os.Open(filename)
		if err != nil {
			if s.Debug {
				log.Printf("Skipped %q: %v", filename, err)
			}
			return
		}
		defer f.Close()
		if s.Verbose {
			fmt.Printf("Now playing: %v\n", filename)
		}
		buf := make([]byte, 64*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				nextFrame <- streamFrame{data: chunk, contentType: ct}
			}
			if err != nil {
				break
			}
		}
	}

	// decodeFile streams one audio file to nextFrame with appropriate pacing.
	// It uses the AudioDecoder.OpenDecode iterator so only one small chunk of
	// raw PCM (≈0.2 s) is resident in memory at a time, regardless of file
	// size.  Normalisation and chunking are applied per-chunk before sending.
	decodeFile := func(filename string, cumwait *time.Duration) {
		dec := decoderForFile(filename)
		if dec == nil {
			return
		}
		f, err := os.Open(filename)
		if err != nil {
			if s.Debug {
				log.Printf("Skipped %q: %v", filename, err)
			}
			return
		}
		defer f.Close()

		rate, ch, next, err := dec.OpenDecode(f)
		if err != nil {
			if s.Debug {
				log.Printf("Skipped %q: decode error: %v", filename, err)
			}
			return
		}
		if s.Verbose {
			fmt.Printf("Now playing: %v\n", filename)
		}

		for {
			raw := next()
			if raw == nil {
				break
			}
			for _, frame := range chunk(normalise(raw, rate, ch), 8820) {
				t0 := time.Now()
				frameBytes := int16sToBytes(frame)
				nextFrame <- streamFrame{data: frameBytes, contentType: "audio/wav"}
				// frame duration = samples / (channels * sample_rate)
				towait := time.Duration(len(frameBytes))*time.Second/(2*2*canonRate) - time.Since(t0)
				*cumwait += towait
				// Clamp cumwait to zero when it goes negative. A large negative
				// value (e.g. -44 s after a broadcastTimeout stall) would cause
				// every subsequent frame to be sent without any sleep, flooding
				// all remaining clients for hours before the deficit is repaid.
				if *cumwait < 0 {
					*cumwait = 0
				}
				if *cumwait > time.Second {
					time.Sleep(*cumwait)
					*cumwait = 0
				}
			}
		}
	}

	// stdin path: decode the stream piped to standard input using the OpenDecode
	// iterator.  Each decoded chunk is forwarded to nextFrame immediately (phase 1)
	// so connecting clients hear audio within the first frame (~0.2 s) rather than
	// waiting for the entire stdin to be consumed.  Frames are also appended to
	// allFrames for the replay loop (phase 2) that keeps the server active after
	// stdin is exhausted.
	//
	// Fix for AUDIT FUNCTIONAL MISMATCH "Stdin Mode Buffers Complete Audio Before
	// Starting Playback": the previous implementation collected allFrames in full
	// before entering the pacing loop, blocking all clients for the entire stdin
	// decode duration.  For a 1-hour MP3 that could be several minutes of silence.
	//
	// Note: for infinite stdin streams (piped internet radio) phase 2 is never
	// reached; allFrames grows proportionally to the stream duration but this is
	// unavoidable when replay-after-reconnect is required.
	//
	// The format is determined by Streamer.StdinFormat (default "mp3").
	go func() {
		if path != "-" {
			return
		}
		m.Lock()
		m.currentCT = "audio/wav"
		m.Unlock()
		// Normalise: strip any leading dot, lower-case, then re-add the dot so
		// decoderForFile can match against registered extensions.
		stdinFmt := strings.ToLower(strings.TrimPrefix(s.StdinFormat, "."))
		dec := decoderForFile("." + stdinFmt)
		if dec == nil {
			log.Printf("stdin: unknown format %q; set a supported format via Streamer.StdinFormat (e.g. \"mp3\" or \"flac\")", s.StdinFormat)
			return
		}
		rate, ch, next, err := dec.OpenDecode(os.Stdin)
		if err != nil {
			log.Printf("stdin decode error: %v", err)
			return
		}

		// Phase 1: live passthrough — send each frame to nextFrame as it is decoded
		// and cache it for replay.  Clients connecting during this phase receive
		// audio immediately without waiting for stdin to be fully consumed.
		var allFrames [][]byte
		var cumwait time.Duration
		for {
			raw := next()
			if raw == nil {
				break
			}
			for _, frame := range chunk(normalise(raw, rate, ch), 8820) {
				t0 := time.Now()
				frameBytes := int16sToBytes(frame)
				allFrames = append(allFrames, frameBytes)
				nextFrame <- streamFrame{data: frameBytes, contentType: "audio/wav"}
				towait := time.Duration(len(frameBytes))*time.Second/(2*2*canonRate) - time.Since(t0)
				cumwait += towait
				if cumwait < 0 {
					cumwait = 0
				}
				if cumwait > time.Second {
					time.Sleep(cumwait)
					cumwait = 0
				}
			}
		}
		if len(allFrames) == 0 {
			return
		}
		// Phase 2: replay — loop the cached frames so late-joining clients receive
		// audio after stdin is exhausted.  For a finite stdin (e.g. a single file)
		// this keeps the server active indefinitely.  For an infinite stdin this
		// loop is never reached.
		for {
			cumwait = 0
			for _, frameBytes := range allFrames {
				t0 := time.Now()
				nextFrame <- streamFrame{data: frameBytes, contentType: "audio/wav"}
				towait := time.Duration(len(frameBytes))*time.Second/(2*2*canonRate) - time.Since(t0)
				cumwait += towait
				if cumwait < 0 {
					cumwait = 0
				}
				if cumwait > time.Second {
					time.Sleep(cumwait)
					cumwait = 0
				}
			}
		}
	}()

	// file path: open, decode/stream, and pace each queued file.
	go func() {
		if path == "-" {
			return
		}
		var cumwait time.Duration
		for {
			entry := <-nextFile
			m.Lock()
			m.currentCT = entry.ContentType
			m.Unlock()
			if entry.ContentType == "audio/wav" {
				decodeFile(entry.Path, &cumwait)
			} else {
				streamVideoFile(entry.Path, entry.ContentType)
			}
		}
	}()

	// broadcast frame to clients in parallel.
	//
	// Each frame is dispatched to every subscribed client concurrently so that
	// a single slow or stalled client cannot delay frame delivery to the rest.
	// Delivery time per frame is O(max individual latency) instead of O(sum of
	// latencies) as it was with the previous serial loop.
	//
	// Algorithm:
	//   1. Snapshot m.clients under the lock, then release it before any
	//      blocking sends so subscribe() is never blocked by a slow write.
	//   2. Spawn one goroutine per client to send the frame; each goroutine
	//      exits as soon as ServeHTTP reads the frame from its channel.
	//   3. Wait for exactly len(snapshot) broadcastResult values to arrive on
	//      m.result — ServeHTTP sends one after writing the chunk (or timing out).
	//   4. Acquire the lock only to mutate m.clients for errored clients.
	go func() {
		type clientSnapshot struct {
			qid int
			ch  chan streamFrame
			ct  string // expected content type for this client
		}
		for {
			f := <-nextFrame

			// 1. Snapshot the current client set.
			m.Lock()
			snapshot := make([]clientSnapshot, 0, len(m.clients))
			for qid, e := range m.clients {
				snapshot = append(snapshot, clientSnapshot{qid, e.ch, e.ct})
			}
			m.Unlock()

			if len(snapshot) == 0 {
				continue
			}

			// 2. Send the frame only to clients whose expected content type
			// matches this frame's content type.  This prevents raw video bytes
			// from being injected into an audio stream when a mixed-content
			// directory transitions from audio to video or vice versa.
			// Every matching handler goroutine is independently waiting on its
			// frames channel so all sends complete concurrently.
			//
			// Clients with a mismatched content type (e.g. an audio/wav client
			// receiving a video frame) are disconnected immediately via
			// unsubscribe so that ServeHTTP sees ok==false on its next <-frames
			// receive and exits cleanly.  The browser then reconnects and
			// receives the correct content type for the new medium.  Without
			// this, audio clients stall indefinitely during video playback
			// (AUDIT edge case bug "Audio Clients Stall Indefinitely When
			// Playlist Transitions to Video").
			sent := 0
			var toDisconnect []int
			for _, e := range snapshot {
				if e.ct != f.contentType {
					toDisconnect = append(toDisconnect, e.qid)
					continue
				}
				sent++
				go func(qid int, ch chan streamFrame, frame streamFrame) {
					// Recover from a "send on closed channel" panic.  This
					// happens when ServeHTTP fails to write the WAV header and
					// calls unsubscribe() which closes ch.  We send an error
					// result to m.result so the broadcast goroutine's expected
					// 'sent' count is always satisfied and it does not hang
					// waiting for a result that ServeHTTP will never send.
					// m.result is buffered so this send never blocks even when
					// the broadcast goroutine has not yet entered its collect
					// phase.  See: AUDIT edge case bug #4.
					defer func() {
						if r := recover(); r != nil {
							m.result <- broadcastResult{qid, fmt.Errorf("channel closed: %v", r)}
						}
					}()
					ch <- frame
				}(e.qid, e.ch, f)
			}
			// Disconnect mismatched clients after all matching dispatch goroutines
			// are launched.  unsubscribe holds m.Lock itself and is safe to call
			// here because no dispatch goroutine was spawned for these clients
			// (they were skipped above), so there is no concurrent send to their
			// channels that could race with the close inside unsubscribe.
			for _, qid := range toDisconnect {
				m.unsubscribe(qid)
			}

			// 3. Collect one result per client we actually sent to.  ServeHTTP
			// sends a broadcastResult after writing (nil error) or on timeout /
			// network error (non-nil error).
			for i := 0; i < sent; i++ {
				br := <-m.result
				if br.err == nil {
					continue
				}
				// 4. Remove the failing client.  Guard with an existence check
				// to handle the unlikely case of a duplicate error result.
				m.Lock()
				if _, ok := m.clients[br.qid]; ok {
					close(m.clients[br.qid].ch)
					delete(m.clients, br.qid)
				}
				nclients := len(m.clients)
				m.Unlock()
				if s.Debug {
					log.Printf("Connection exited, qid: %v, error %v. Now streaming to %v connections.", br.qid, br.err, nclients)
				} else if s.Verbose {
					fmt.Printf("Connection exited, qid: %v. Now streaming to %v connections, at %v\n", br.qid, nclients, time.Now().Format(time.Stamp))
				}
			}
		}
	}()

	return m
}
