package lib

import "encoding/binary"

// wavHeader returns the 44-byte RIFF/WAV header for an open-ended PCM stream.
// The data-chunk size is set to 0xFFFFFFFF to signal an unbounded stream.
// Callers write this header once at the start of the HTTP response and then
// stream normalised PCM chunks without any further framing.
func wavHeader(sampleRate, channels, bitsPerSample uint32) []byte {
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := uint16(channels * bitsPerSample / 8)

	buf := make([]byte, 44)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], 0xFFFFFFFF) // open-ended RIFF chunk
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16) // fmt chunk length
	binary.LittleEndian.PutUint16(buf[20:], 1)  // PCM = 1
	binary.LittleEndian.PutUint16(buf[22:], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:], byteRate)
	binary.LittleEndian.PutUint16(buf[32:], blockAlign)
	binary.LittleEndian.PutUint16(buf[34:], uint16(bitsPerSample))
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:], 0xFFFFFFFF) // open-ended data chunk
	return buf
}
