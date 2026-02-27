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
