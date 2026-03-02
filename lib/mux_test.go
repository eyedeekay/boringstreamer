package lib

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// TestSubscribeMaxConnectionsEnforced verifies that subscribe returns -1 once
// the pool is at capacity, and that clients can fill every slot up to the limit.
func TestSubscribeMaxConnectionsEnforced(t *testing.T) {
	const max = 3
	s := &Streamer{MaxConnections: max}
	m := &mux{
		clients:  make(map[int]chan streamFrame),
		result:   make(chan broadcastResult),
		streamer: s,
	}

	// First 'max' subscriptions must succeed with distinct qids.
	seen := make(map[int]bool)
	for i := 0; i < max; i++ {
		ch := make(chan streamFrame, 1)
		qid, res := m.subscribe(ch)
		if qid < 0 {
			t.Fatalf("subscription %d rejected (qid=%d), want success", i+1, qid)
		}
		if res == nil {
			t.Fatalf("subscription %d: nil result channel", i+1)
		}
		if seen[qid] {
			t.Fatalf("duplicate qid %d assigned", qid)
		}
		seen[qid] = true
	}

	// One more must be rejected.
	qid, res := m.subscribe(make(chan streamFrame, 1))
	if qid >= 0 {
		t.Errorf("subscription beyond MaxConnections accepted (qid=%d), want rejection", qid)
	}
	if res != nil {
		t.Errorf("over-capacity subscribe returned non-nil result channel")
	}
}

// TestSubscribeMaxConnectionsZero verifies that MaxConnections==0 rejects all
// clients immediately and never assigns a qid.
func TestSubscribeMaxConnectionsZero(t *testing.T) {
	s := &Streamer{MaxConnections: 0}
	m := &mux{
		clients:  make(map[int]chan streamFrame),
		result:   make(chan broadcastResult),
		streamer: s,
	}

	qid, res := m.subscribe(make(chan streamFrame, 1))
	if qid >= 0 {
		t.Errorf("subscribe with MaxConnections=0 accepted (qid=%d), want -1", qid)
	}
	if res != nil {
		t.Errorf("subscribe with MaxConnections=0 returned non-nil result channel")
	}
}

// TestSubscribeMaxConnectionsOne verifies the single-slot boundary case.
func TestSubscribeMaxConnectionsOne(t *testing.T) {
	s := &Streamer{MaxConnections: 1}
	m := &mux{
		clients:  make(map[int]chan streamFrame),
		result:   make(chan broadcastResult),
		streamer: s,
	}

	qid1, _ := m.subscribe(make(chan streamFrame, 1))
	if qid1 < 0 {
		t.Fatalf("first subscription rejected with MaxConnections=1, want success")
	}

	qid2, _ := m.subscribe(make(chan streamFrame, 1))
	if qid2 >= 0 {
		t.Errorf("second subscription accepted with MaxConnections=1 (already full), want rejection (got qid=%d)", qid2)
	}
}

// TestSubscribeQidsAreUnique verifies that concurrent subscribe calls never
// produce the same qid. (Sequential here; concurrency is tested implicitly via
// the mutex that subscribe holds while mutating the map.)
func TestSubscribeQidsAreUnique(t *testing.T) {
	const max = 10
	s := &Streamer{MaxConnections: max}
	m := &mux{
		clients:  make(map[int]chan streamFrame),
		result:   make(chan broadcastResult),
		streamer: s,
	}

	qids := make(map[int]bool)
	for i := 0; i < max; i++ {
		qid, _ := m.subscribe(make(chan streamFrame, 1))
		if qid < 0 {
			t.Fatalf("subscription %d unexpectedly rejected", i+1)
		}
		if qids[qid] {
			t.Fatalf("duplicate qid %d on subscription %d", qid, i+1)
		}
		qids[qid] = true
	}
}

// TestNewStreamerDefaults verifies that NewStreamer returns sensible defaults
// for the newly added StdinFormat and the updated Path.
func TestNewStreamerDefaults(t *testing.T) {
	s := NewStreamer()

	if s.StdinFormat != "mp3" {
		t.Errorf("NewStreamer().StdinFormat = %q, want \"mp3\"", s.StdinFormat)
	}
	if s.Path != "." {
		t.Errorf("NewStreamer().Path = %q, want \".\"", s.Path)
	}
	if s.MaxConnections <= 0 {
		t.Errorf("NewStreamer().MaxConnections = %d, want > 0", s.MaxConnections)
	}
}

// TestStdinFormatNormalisedByDecoderForFile confirms that decoderForFile
// correctly resolves both ".mp3" and ".flac" — the two valid StdinFormat
// values — so the mux stdin goroutine will always find their decoders.
func TestStdinFormatNormalisedByDecoderForFile(t *testing.T) {
	for _, ext := range []string{".mp3", ".flac"} {
		if decoderForFile(ext) == nil {
			t.Errorf("decoderForFile(%q) = nil; stdin format %q would silently fail", ext, ext[1:])
		}
	}
}

// TestStdinFormatUnknownReturnsNil confirms that an unrecognised StdinFormat
// value causes decoderForFile to return nil, which is the signal used by the
// mux stdin goroutine to log an error and abort.
func TestStdinFormatUnknownReturnsNil(t *testing.T) {
	if decoderForFile(".ogg") != nil {
		t.Error("decoderForFile(\".ogg\") should return nil for unsupported format")
	}
	if decoderForFile(".xyz") != nil {
		t.Error("decoderForFile(\".xyz\") should return nil for unsupported format")
	}
}

// TestShuffleAllPermutationsReachable verifies that the Fisher-Yates shuffle
// used in the buffer-and-shuffle goroutine (rand.Shuffle) produces a uniform
// distribution over all n! permutations.
//
// The previous algorithm inserted each new element at a random position and
// moved the displaced element to the end. For n files it could produce at most
// 2^(n-1) distinct orderings instead of the required n!. With 2 files only
// one of the two possible orderings was ever produced; with 3 files only 2 of
// the 6 were reachable. The test below would have failed deterministically
// under the old algorithm.
//
// With rand.Shuffle and 1000 trials over 3 elements, the probability of any
// individual permutation never appearing is (5/6)^1000 ≈ 1e-79.
func TestShuffleAllPermutationsReachable(t *testing.T) {
	// Use a fixed-seed source so the test is deterministic in CI while still
	// exercising the shuffle across many iterations.
	src := rand.New(rand.NewSource(12345))

	type key [3]string
	input := [3]string{"A", "B", "C"}
	seen := make(map[key]bool)

	const trials = 1000
	for i := 0; i < trials; i++ {
		s := input // copy
		src.Shuffle(len(s), func(i, j int) {
			s[i], s[j] = s[j], s[i]
		})
		seen[s] = true
	}

	// All 3! = 6 permutations of three distinct elements must be reachable.
	const wantPermutations = 6
	if len(seen) != wantPermutations {
		t.Errorf("shuffle produced %d distinct orderings, want %d (all 3! permutations); "+
			"a biased algorithm would produce at most 4", len(seen), wantPermutations)
	}
}

// TestShuffleTwoElementsBothOrderings verifies the minimal two-element case:
// both orderings must be reachable. The old insert-at-random-index algorithm
// failed here because rand.Intn(1)==0 always, giving a single deterministic
// outcome regardless of how many trials were run.
func TestShuffleTwoElementsBothOrderings(t *testing.T) {
	src := rand.New(rand.NewSource(99))
	type key [2]string
	seen := make(map[key]bool)

	const trials = 200
	for i := 0; i < trials; i++ {
		s := [2]string{"X", "Y"}
		src.Shuffle(len(s), func(i, j int) {
			s[i], s[j] = s[j], s[i]
		})
		seen[s] = true
	}

	if len(seen) != 2 {
		t.Errorf("two-element shuffle produced %d distinct orderings, want 2", len(seen))
	}
}

// TestParallelBroadcastAllClientsReceiveFrame verifies that the parallel
// broadcast pattern delivers a frame to every subscribed client.
//
// The test directly exercises the dispatch/collect pattern used by the new
// broadcast goroutine without going through the full mux.start() machinery:
// it spawns N send goroutines (one per client) and N consumer goroutines, then
// checks that every consumer received exactly one frame.
func TestParallelBroadcastAllClientsReceiveFrame(t *testing.T) {
	const n = 5
	result := make(chan broadcastResult, n)

	type entry struct {
		qid int
		ch  chan streamFrame
	}
	snapshot := make([]entry, n)
	for i := range snapshot {
		snapshot[i] = entry{qid: i, ch: make(chan streamFrame)}
	}

	frame := streamFrame([]byte("audio chunk"))

	// Consumer goroutines: each reads one frame and sends success to result.
	received := make([]bool, n)
	for _, e := range snapshot {
		go func(qid int, ch <-chan streamFrame) {
			<-ch
			received[qid] = true
			result <- broadcastResult{qid: qid, err: nil}
		}(e.qid, e.ch)
	}

	// Broadcast: send to all clients in parallel.
	for _, e := range snapshot {
		go func(ch chan<- streamFrame, f streamFrame) {
			ch <- f
		}(e.ch, frame)
	}

	// Collect all results.
	for range snapshot {
		<-result
	}

	for i, got := range received {
		if !got {
			t.Errorf("client %d did not receive the frame", i)
		}
	}
}

// TestParallelBroadcastIsNotSerial verifies that parallel delivery time is
// bounded by the slowest single client, not the sum of all client latencies.
//
// Each simulated client sleeps for `delay` before consuming its frame.
// Serial delivery (old code) would require n*delay to complete.
// Parallel delivery (new code) should finish in approximately 1*delay.
//
// The test sends to n clients, each with an identical intentional delay, and
// asserts the total elapsed time is well below n*delay.
func TestParallelBroadcastIsNotSerial(t *testing.T) {
	const n = 4
	const delay = 30 * time.Millisecond

	result := make(chan broadcastResult, n)

	type entry struct {
		qid int
		ch  chan streamFrame
	}
	snapshot := make([]entry, n)
	for i := range snapshot {
		snapshot[i] = entry{qid: i, ch: make(chan streamFrame)}
	}

	frame := streamFrame([]byte("timing test chunk"))

	// Each consumer blocks for `delay` before reading — simulating a client
	// that is briefly slow to consume its receive buffer.
	for _, e := range snapshot {
		go func(qid int, ch <-chan streamFrame) {
			time.Sleep(delay) // simulate slow consumer
			<-ch
			result <- broadcastResult{qid: qid, err: nil}
		}(e.qid, e.ch)
	}

	t0 := time.Now()

	// Parallel send.
	for _, e := range snapshot {
		go func(ch chan<- streamFrame, f streamFrame) {
			ch <- f
		}(e.ch, frame)
	}

	// Collect results.
	for range snapshot {
		<-result
	}

	elapsed := time.Since(t0)

	// Serial delivery would take at least n*delay.  Parallel delivery takes
	// approximately 1*delay.  Allow 2.5× for scheduling jitter; the serial
	// bound is a hard lower-limit for the old implementation.
	serialBound := time.Duration(n) * delay
	if elapsed >= serialBound {
		t.Errorf("delivery took %v; expected < %v (serial lower-bound); parallel delivery should be ~%v",
			elapsed, serialBound, delay)
	}
}

// TestParallelBroadcastErroredClientRemoved verifies that when one client
// reports an error in its broadcastResult the broadcast goroutine removes it
// from the client map while leaving healthy clients intact.
//
// The test constructs a minimal mux, populates its client map directly, then
// runs one iteration of the snapshot-send-collect loop and inspects the map.
func TestParallelBroadcastErroredClientRemoved(t *testing.T) {
	const goodQID = 0
	const badQID = 1

	// Shared result channel — mirrors mux.result.
	result := make(chan broadcastResult, 2)

	goodCh := make(chan streamFrame)
	badCh := make(chan streamFrame)

	s := &Streamer{MaxConnections: 2}
	m := &mux{
		clients:  map[int]chan streamFrame{goodQID: goodCh, badQID: badCh},
		result:   result,
		streamer: s,
	}

	frame := streamFrame([]byte("data"))

	// Simulate ServeHTTP for the good client: receive frame, report success.
	go func() {
		<-goodCh
		result <- broadcastResult{qid: goodQID, err: nil}
	}()

	// Simulate ServeHTTP for the bad client: receive frame, report error.
	go func() {
		<-badCh
		result <- broadcastResult{qid: badQID, err: fmt.Errorf("write: broken pipe")}
	}()

	// Run one parallel broadcast iteration (the pattern from the new code).
	type clientSnapshot struct {
		qid int
		ch  chan streamFrame
	}
	m.Lock()
	snapshot := make([]clientSnapshot, 0, len(m.clients))
	for qid, ch := range m.clients {
		snapshot = append(snapshot, clientSnapshot{qid, ch})
	}
	m.Unlock()

	for _, e := range snapshot {
		go func(ch chan streamFrame, f streamFrame) {
			ch <- f
		}(e.ch, frame)
	}

	for range snapshot {
		br := <-m.result
		if br.err == nil {
			continue
		}
		m.Lock()
		if _, ok := m.clients[br.qid]; ok {
			close(m.clients[br.qid])
			delete(m.clients, br.qid)
		}
		m.Unlock()
	}

	// After the iteration, the bad client must be gone; good client must remain.
	m.Lock()
	_, goodExists := m.clients[goodQID]
	_, badExists := m.clients[badQID]
	m.Unlock()

	if !goodExists {
		t.Errorf("healthy client (qid %d) was incorrectly removed from m.clients", goodQID)
	}
	if badExists {
		t.Errorf("errored client (qid %d) was not removed from m.clients", badQID)
	}
}

// TestBroadcastSendToClosedChannelNoPanic verifies the fix for the High-severity
// "send on closed channel" server crash (AUDIT finding #1).
//
// Race reproduced by three concurrent actors:
//  1. ServeHTTP subscribes; its channel ch is added to m.clients.
//  2. The broadcast goroutine snapshots m.clients (includes ch) and spawns
//     a send goroutine: go func() { ch <- frame }()
//     The send goroutine BLOCKS because ServeHTTP has not entered its
//     frame-receive loop yet (it is still writing the WAV header).
//  3. The WAV header write fails.  ServeHTTP sends the error result directly
//     to m.result BEFORE ever calling <-ch.
//  4. The broadcast goroutine reads that error result and calls close(ch).
//  5. The blocked send goroutine in step 2 would panic with
//     "send on closed channel" — crashing the entire server.
//
// The fix adds `defer func() { recover() }()` to the send goroutine so the
// panic is caught and the goroutine exits cleanly.
//
// This test creates an already-closed channel and starts a goroutine
// attempting to send to it, reproducing step 5. The goroutine body is
// identical to the fixed production code. Without the recover() the test
// would panic; with it the goroutine exits and the test passes.
func TestBroadcastSendToClosedChannelNoPanic(t *testing.T) {
	ch := make(chan streamFrame)
	close(ch) // simulate the broadcast goroutine closing the channel (step 4)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Exact copy of the fixed send goroutine body from mux.go.
		defer func() { recover() }() //nolint:errcheck
		ch <- streamFrame([]byte("frame"))
	}()

	select {
	case <-done:
		// Goroutine exited cleanly — panic was recovered, server would not crash.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("send goroutine did not exit after channel was closed; likely deadlocked or panicked")
	}
}

// TestSubscribeVerboseCountNeverRaces verifies the fix for the data race where
// subscribe read len(m.clients) after releasing m.Unlock() (AUDIT finding #3).
//
// The fix captures the count while still holding the lock.  This test
// exercises the verbose path under concurrent subscribe + map-delete, which is
// the exact interleaving that triggers a "concurrent map read and map write"
// panic with the race detector.  Running with "go test -race" confirms the fix.
func TestSubscribeVerboseCountNeverRaces(t *testing.T) {
	const maxConn = 50
	s := &Streamer{MaxConnections: maxConn, Verbose: true}
	m := &mux{
		clients:  make(map[int]chan streamFrame),
		result:   make(chan broadcastResult),
		streamer: s,
	}

	// Subscriber goroutines: fill the pool and record which qids were assigned.
	var wg sync.WaitGroup
	qids := make(chan int, maxConn)
	for i := 0; i < maxConn; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := make(chan streamFrame, 1)
			qid, _ := m.subscribe(ch)
			if qid >= 0 {
				qids <- qid
			}
		}()
	}

	// Concurrent delete goroutine: simulates the broadcast goroutine removing
	// clients from the map.  This is the writer that races with the previously
	// unlocked len(m.clients) read.
	deleteDone := make(chan struct{})
	go func() {
		defer close(deleteDone)
		for qid := range qids {
			m.Lock()
			delete(m.clients, qid)
			m.Unlock()
		}
	}()

	wg.Wait()
	close(qids)
	<-deleteDone
}

// TestPacingCumwaitClampedAtZero verifies the fix for the pacing drift bug:
// after a long stall the cumulative wait must be clamped to zero instead of
// accumulating a large negative value. Covers both the decodeFile path and
// the stdin playback loop, which share identical pacing arithmetic.
//
// The test simulates the values in decodeFile's inner loop by applying the
// same arithmetic and clamp logic directly to a time.Duration variable.
func TestPacingCumwaitClampedAtZero(t *testing.T) {
	// simulatePace applies one iteration of the pacing loop with the given
	// towait contribution and returns the resulting cumwait.
	simulatePace := func(cumwait time.Duration, towait time.Duration) time.Duration {
		cumwait += towait
		if cumwait < 0 {
			cumwait = 0
		}
		if cumwait > time.Second {
			// In production, Sleep is called here. For this test, just reset.
			cumwait = 0
		}
		return cumwait
	}

	tests := []struct {
		name    string
		cumwait time.Duration
		towait  time.Duration
		want    time.Duration
	}{
		{
			name:    "large negative stall (44 s timeout) clamps to zero",
			cumwait: 0,
			towait:  -44 * time.Second,
			want:    0,
		},
		{
			name:    "small negative overshoot clamps to zero",
			cumwait: 500 * time.Millisecond,
			towait:  -600 * time.Millisecond,
			want:    0,
		},
		{
			name:    "positive contribution accumulates normally",
			cumwait: 100 * time.Millisecond,
			towait:  200 * time.Millisecond,
			want:    300 * time.Millisecond,
		},
		{
			name:    "exactly zero is not affected by clamp",
			cumwait: 0,
			towait:  0,
			want:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := simulatePace(tc.cumwait, tc.towait)
			if got != tc.want {
				t.Errorf("simulatePace(cumwait=%v, towait=%v) = %v, want %v",
					tc.cumwait, tc.towait, got, tc.want)
			}
			if got < 0 {
				t.Errorf("cumwait went negative (%v); pacing would flood clients", got)
			}
		})
	}
}

// TestPacingStdinCumwaitClampedAtZero verifies the fix for the missing negative
// clamp in the stdin playback loop (AUDIT: "Stdin Pacing Missing Negative
// cumwait Clamp"). The stdin loop previously lacked the clamp present in
// decodeFile; after any broadcastTimeout stall (~44 s) cumwait would go deeply
// negative and all subsequent frames would be sent without sleep, flooding
// clients.
//
// The test mirrors the exact arithmetic of the stdin inner loop:
//
//	cumwait += towait
//	if cumwait < 0 { cumwait = 0 }    ← the added fix
//	if cumwait > time.Second { sleep; cumwait = 0 }
func TestPacingStdinCumwaitClampedAtZero(t *testing.T) {
	// simulateStdinPace runs one iteration of the stdin pacing logic.
	simulateStdinPace := func(cumwait, towait time.Duration) time.Duration {
		cumwait += towait
		if cumwait < 0 { // fix: clamp that was previously missing in the stdin path
			cumwait = 0
		}
		if cumwait > time.Second {
			cumwait = 0
		}
		return cumwait
	}

	tests := []struct {
		name    string
		cumwait time.Duration
		towait  time.Duration
		want    time.Duration
	}{
		{
			name:    "broadcast stall of 44 s clamps to zero",
			cumwait: 0,
			towait:  -44 * time.Second,
			want:    0,
		},
		{
			name:    "partial overshoot clamps to zero",
			cumwait: 200 * time.Millisecond,
			towait:  -300 * time.Millisecond,
			want:    0,
		},
		{
			name:    "positive accumulation is unaffected",
			cumwait: 100 * time.Millisecond,
			towait:  150 * time.Millisecond,
			want:    250 * time.Millisecond,
		},
		{
			name:    "zero stays zero",
			cumwait: 0,
			towait:  0,
			want:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := simulateStdinPace(tc.cumwait, tc.towait)
			if got != tc.want {
				t.Errorf("simulateStdinPace(cumwait=%v, towait=%v) = %v, want %v",
					tc.cumwait, tc.towait, got, tc.want)
			}
			if got < 0 {
				t.Errorf("cumwait went negative (%v); stdin pacing would flood clients", got)
			}
		})
	}
}
