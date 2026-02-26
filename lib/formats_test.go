package lib

import "testing"

func TestContentTypeForVideoFile(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"movie.mp4", "video/mp4", true},
		{"clip.webm", "video/webm", true},
		{"old.avi", "video/x-msvideo", true},
		{"film.mkv", "video/x-matroska", true},
		{"UPPER.MP4", "video/mp4", true}, // case-insensitive
		{"song.mp3", "", false},
		{"doc.txt", "", false},
		{"noext", "", false},
	}
	for _, c := range cases {
		got, ok := contentTypeForVideoFile(c.name)
		if ok != c.wantOK {
			t.Errorf("contentTypeForVideoFile(%q) ok=%v, want %v", c.name, ok, c.wantOK)
		}
		if got != c.want {
			t.Errorf("contentTypeForVideoFile(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestContentTypeForFile(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"song.mp3", "audio/wav"},
		{"lossless.flac", "audio/wav"},
		{"SONG.MP3", "audio/wav"}, // case-insensitive
		{"video.mp4", "video/mp4"},
		{"video.webm", "video/webm"},
		{"unknown.xyz", ""},
		{"noext", ""},
	}
	for _, c := range cases {
		got := contentTypeForFile(c.name)
		if got != c.want {
			t.Errorf("contentTypeForFile(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDecoderForFile(t *testing.T) {
	if decoderForFile("track.mp3") == nil {
		t.Error("expected non-nil decoder for .mp3")
	}
	if decoderForFile("track.flac") == nil {
		t.Error("expected non-nil decoder for .flac")
	}
	if decoderForFile("TRACK.MP3") == nil {
		t.Error("expected non-nil decoder for .MP3 (should be case-insensitive)")
	}
	if decoderForFile("track.txt") != nil {
		t.Error("expected nil decoder for .txt")
	}
	if decoderForFile("track.mp4") != nil {
		t.Error("expected nil decoder for .mp4 (video, not audio)")
	}
}
