// Author: Gergely Födémesi fgergo@gmail.com

/*
Boringstreamer looks for mp3 files and broadcasts via http (live streaming.)

	$ boringstreamer

or

	c:\>boringstreamer.exe

recursively looks for .mp3 files starting from "/" and broadcasts on port 4444 for
at most 42 concurrent http clients.

Details: see -h.

Browse to listen (e.g. http://localhost:4444/)

# Bugs

A browser or player feature/bug:  Usually happens when boringstreamer streams
different mp3s with different sample rates (e.g. 44100 and 48000). If the sample
rate or bitrate or number of channels changes during playing, the player stops
playing the stream. Boringstreamer handles the different files, but the players stop playing mp3s.

Workaround 1: Refresh page in the browser when mp3 playing is stopped.

Workaround 2: Change all mp3s to uniform format. Doesn't matter which format, it just should be uniform.
For example with ffmpeg:

	ffmpeg -i source.mp3 -vn -ar 44100 -ac 2 -ab 128 -f mp3 output.mp3
*/
package lib
