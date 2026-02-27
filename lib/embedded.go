// Package lib provides an embeddable boringstreamer HTTP audio streaming service.
package lib

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"

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
	// (.mp3, .flac, .mp4, .webm, .avi, .mkv), or "-" to stream stdin (MP3).
	Path string

	server *http.Server
}

// NewStreamer returns a Streamer initialised with sensible defaults.
func NewStreamer() *Streamer {
	return &Streamer{
		MaxConnections: 42,
		Recursive:      true,
		Path:           "/",
	}
}

// ListenAndServe opens a TCP listener on addr (e.g. ":4444") and then calls
// Serve. It is a convenience wrapper around Serve for the common case.
func (s *Streamer) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve accepts connections on ln and streams audio to each client.
// The listening address is retrieved from ln.Addr() and is used only for
// informational log output; it is never stored as a string field.
func (s *Streamer) Serve(ln net.Listener) error {
	if s.Debug {
		go func() {
			log.Println(http.ListenAndServe(":6060", nil))
		}()
	}
	if s.Verbose {
		fmt.Printf("Waiting for connections on %v\n", ln.Addr())
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
