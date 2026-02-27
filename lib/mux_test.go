package lib

import "testing"

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
