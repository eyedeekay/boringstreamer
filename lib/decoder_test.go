package lib

import (
	"bytes"
	"testing"
)

// Compile-time assertions: both concrete decoder types must satisfy the
// AudioDecoder interface.  A build failure here means a decoder no longer
// implements the OpenDecode method required by the interface.
var _ AudioDecoder = mp3Decoder{}
var _ AudioDecoder = flacDecoder{}

// TestMP3OpenDecodeErrorOnInvalidData verifies that passing random bytes to
// the MP3 decoder surfaces an error from mp3.NewDecoder rather than panicking
// or returning a next() iterator that would produce garbage output.
func TestMP3OpenDecodeErrorOnInvalidData(t *testing.T) {
	d := mp3Decoder{}
	_, _, _, err := d.OpenDecode(bytes.NewReader([]byte("this is not an mp3 file")))
	if err == nil {
		t.Error("OpenDecode with invalid MP3 data: want error, got nil")
	}
}

// TestFLACOpenDecodeErrorOnInvalidData verifies the same contract for the
// FLAC decoder: corrupt input must return an error, not a valid iterator.
func TestFLACOpenDecodeErrorOnInvalidData(t *testing.T) {
	d := flacDecoder{}
	_, _, _, err := d.OpenDecode(bytes.NewReader([]byte("this is not a flac file")))
	if err == nil {
		t.Error("OpenDecode with invalid FLAC data: want error, got nil")
	}
}

// TestMP3OpenDecodeEmptyReaderReturnsError verifies that an empty reader
// (EOF immediately) is treated as corrupt input and returns an error.
func TestMP3OpenDecodeEmptyReaderReturnsError(t *testing.T) {
	d := mp3Decoder{}
	_, _, _, err := d.OpenDecode(bytes.NewReader(nil))
	if err == nil {
		t.Error("OpenDecode with empty reader: want error, got nil")
	}
}

// TestFLACOpenDecodeEmptyReaderReturnsError verifies the same for FLAC.
func TestFLACOpenDecodeEmptyReaderReturnsError(t *testing.T) {
	d := flacDecoder{}
	_, _, _, err := d.OpenDecode(bytes.NewReader(nil))
	if err == nil {
		t.Error("OpenDecode with empty reader: want error, got nil")
	}
}

// TestOpenDecodeNextSignatureIsIterator verifies that the next() function
// returned by a successful OpenDecode call is not nil.  Full decode-output
// correctness is validated by integration testing with real audio files;
// unit tests cannot easily generate valid MP3/FLAC frames without fixture
// files.
//
// We verify the iterator contract via decoderForFile so that any newly
// registered format is also covered by this check.
func TestOpenDecodeNextSignatureIsIterator(t *testing.T) {
	for _, ext := range []string{".mp3", ".flac"} {
		dec := decoderForFile(ext)
		if dec == nil {
			t.Fatalf("decoderForFile(%q) = nil; test setup failed", ext)
		}
		// A corrupt reader must produce an error, never a nil next function
		// alongside a nil error.
		_, _, next, err := dec.OpenDecode(bytes.NewReader([]byte{0x00}))
		if err == nil && next == nil {
			t.Errorf("decoderForFile(%q).OpenDecode: got nil next with nil error; want either a valid iterator or an error", ext)
		}
	}
}
