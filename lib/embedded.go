// Package lib provides an embeddable boringstreamer HTTP audio streaming service.
package lib

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	_ "net/http/pprof" // imported for debug profiling endpoint
)

// Streamer is the top-level handle for a boringstreamer instance.
// All configuration is expressed as plain struct fields; set them
// before calling ListenAndServe or Serve.
type Streamer struct {
	// MaxConnections is the maximum number of simultaneous streaming clients.
	MaxConnections int
	// Recursive controls whether subdirectories are scanned for audio and video files.
	// When false only the top-level directory is searched; subdirectories are skipped.
	Recursive bool
	// Verbose enables informational log output.
	Verbose bool
	// Debug enables additional debug logging and exposes a pprof endpoint on :6060.
	Debug bool
	// Path is the file-system path to scan for audio and video files
	// (.mp3, .flac, .mp4, .webm, .avi, .mkv), or "-" to stream stdin.
	// Defaults to "." (the current working directory).
	Path string
	// StdinFormat specifies the audio format expected on standard input when
	// Path is "-".  Valid values are the registered audio format extensions
	// without the leading dot: "mp3" or "flac".  The value is case-insensitive.
	// Defaults to "mp3".
	StdinFormat string

	server *http.Server
}

// NewStreamer returns a Streamer initialised with sensible defaults.
func NewStreamer() *Streamer {
	return &Streamer{
		MaxConnections: 42,
		Recursive:      true,
		Path:           ".",
		StdinFormat:    "mp3",
	}
}

// validateConfig checks that the Streamer configuration is valid before the
// server starts.  It returns a descriptive error for unsupported or
// inconsistent settings so callers receive a clear message rather than a
// silent runtime failure.
//
// Currently validates: when Path is "-", StdinFormat must name a registered
// audio decoder (e.g. "mp3" or "flac").  An unrecognised format would cause
// the stdin goroutine to exit silently, leaving the server running but
// delivering silence to every connecting client.
func (s *Streamer) validateConfig() error {
	if s.Path == "-" {
		stdinFmt := strings.ToLower(strings.TrimPrefix(s.StdinFormat, "."))
		if decoderForFile("."+stdinFmt) == nil {
			return fmt.Errorf("stdin: unknown format %q; supported audio formats: mp3, flac", s.StdinFormat)
		}
	}
	return nil
}

// ListenAndServe opens a TCP listener on addr (e.g. ":4444") and then calls
// Serve. It is a convenience wrapper around Serve for the common case.
func (s *Streamer) ListenAndServe(addr string) error {
	if err := s.validateConfig(); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve accepts connections on ln and streams audio to each client.
// The listening address is retrieved from ln.Addr() and is used only for
// informational log output; it is never stored as a string field.
//
// Serve validates the Streamer configuration before starting the HTTP server,
// matching the guarantee provided by ListenAndServe. Callers who construct
// their own net.Listener (e.g. TLS wrappers) receive the same early error
// feedback rather than a silently broken stream.
func (s *Streamer) Serve(ln net.Listener) error {
	if err := s.validateConfig(); err != nil {
		return err
	}
	if s.Debug {
		go func() {
			log.Println(http.ListenAndServe(":6060", nil))
		}()
	}
	if s.Verbose {
		log.Printf("Waiting for connections on %v", ln.Addr())
	}

	s.server = &http.Server{
		Handler: streamHandler{new(mux).start(s)},
	}
	return s.server.Serve(ln)
}

// Shutdown gracefully stops the HTTP server using the provided context.
// It does not interrupt active streaming connections immediately; callers
// should supply an appropriate deadline via ctx.
func (s *Streamer) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}
