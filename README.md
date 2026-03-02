Sensible defaults -> no configuration files.

# Boringstreamer streams audio and video files via HTTP

Boringstreamer scans a directory for audio and video files and broadcasts them
via HTTP live streaming. Browse to listen or watch (e.g. http://localhost:4444/)

Supported formats: `.mp3`, `.flac`, `.mp4`, `.webm`, `.avi`, `.mkv`

All audio (MP3 and FLAC) is normalised to a single continuous 44100 Hz stereo
WAV stream, so mixed sample rates and mono/stereo files play back seamlessly.

# Install

    go install github.com/fgergo/boringstreamer@latest

# Run

    $ boringstreamer /path/to/music

or pipe an MP3 stream directly:

    $ cat file.mp3 | boringstreamer -

then open Chrome, Firefox, etc. to listen.

# Help

Use -h flag.

# Developer Notes

## Debug mode

Passing `-debug` as a second positional argument enables extra debug logging
and exposes a [pprof](https://pkg.go.dev/net/http/pprof) profiling endpoint
on `:6060`:

    boringstreamer /path/to/music -debug

This is intentionally hidden from the regular `-h` output because it is a
developer/operator tool, not an end-user feature.
