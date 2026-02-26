// Author: Gergely Födémesi fgergo@gmail.com

/*
Boringstreamer looks for audio files and broadcasts them via HTTP live streaming.

	$ boringstreamer

or

	c:\>boringstreamer.exe

Recursively looks for supported audio files starting from "/" and broadcasts on
port 4444 for at most 42 concurrent http clients.

Details: see -h.

Browse to listen (e.g. http://localhost:4444/)

All audio is decoded to raw PCM, normalised to 44100 Hz stereo, and streamed as
a single continuous WAV response. Sample-rate and channel differences between
files are handled transparently; there is no need to pre-process files to a
uniform format.

Supported audio formats: MP3, FLAC.

Supported video formats: MP4 and WebM play inline in all major browsers.
AVI and MKV are passed through but will trigger a download in most browsers
rather than inline playback.
*/
package lib
