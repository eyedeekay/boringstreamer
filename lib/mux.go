// mux handles broadcasting audio streams to multiple subscribed clients.
package lib

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// mux broadcasts audio stream to subscribed clients (ie. to http servers).
// Clients subscribe() and unsubscribe by writing to result channel.
type mux struct {
	sync.Mutex

	clients  map[int]chan streamFrame // set of listener clients to be notified
	result   chan broadcastResult     // clients share broadcast success-failure here
	streamer *Streamer
}

// subscribe adds ch to the set of channels to be received on by the clients when a new audio frame is available.
// Returns unique client id (qid) for ch and a broadcast result channel for the client.
// Returns -1, nil if too many clients are already listening.
func (m *mux) subscribe(ch chan streamFrame) (int, chan broadcastResult) {
	m.Lock()
	// search for available qid
	qid := 0
	_, ok := m.clients[qid]
	for ; ok; _, ok = m.clients[qid] {
		if qid >= m.streamer.MaxConnections-1 {
			m.Unlock()
			return -1, nil
		}
		qid++
	}
	m.clients[qid] = ch
	m.Unlock()
	if m.streamer.Verbose {
		fmt.Printf("New connection (qid: %v), streaming to %v connections, at %v\n", qid, len(m.clients), time.Now().Format(time.Stamp))
	}

	return qid, m.result
}

// start initializes a multiplexer for raw audio streams using the provided Streamer config.
// e.g: m := new(mux).start(s)
func (m *mux) start(s *Streamer) *mux {
	m.streamer = s
	path := s.Path

	m.result = make(chan broadcastResult)
	m.clients = make(map[int]chan streamFrame)

	// flow structure: fs -> nextFile -> nextFrame -> subscribed http servers -> browsers
	nextFile := make(chan string)       // next file to be broadcast
	nextFrame := make(chan streamFrame) // next audio frame

	// generate randomized list of files available from path
	rand.Seed(time.Now().Unix()) // minimal randomness
	rescan := make(chan chan string)
	go func() {
		if path == "-" {
			return
		}

		for {
			files := <-rescan

			t0 := time.Now()
			notified := false
			filepath.Walk(path, func(wpath string, info os.FileInfo, err error) error {
				// notify user if no audio files are found after 4 seconds of walking path recursively
				dt := time.Now().Sub(t0)
				if dt > 4*time.Second && !notified && s.Verbose {
					fmt.Printf("Still looking for first audio file under %#v to broadcast, after %v... Maybe try -h flag.\n", path, dt)
					notified = true
				}

				if err != nil {
					return nil
				}
				if !info.Mode().IsRegular() {
					return nil
				}
				if !info.IsDir() && decoderForFile(info.Name()) == nil {
					return nil
				}

				files <- wpath // found file

				return nil
			})
			close(files)
			time.Sleep(1 * time.Second) // if no files are found, poll at least with 1Hz
		}
	}()

	// buffer and shuffle
	go func() {
		if path == "-" {
			return
		}

		for {
			files := make(chan string)
			rescan <- files

			shuffled := make([]string, 0) // randomized set of files

			for f := range files {
				select {
				case <-time.After(100 * time.Millisecond): // start playing as soon as possible, but wait at least 0.1 second for shuffling
					nextFile <- f
					if s.Verbose {
						fmt.Printf("Next: %v\n", f)
					}
				default:
					// shuffle files for random playback
					// (random permutation)
					if len(shuffled) == 0 {
						shuffled = append(shuffled, f)
					} else {
						i := rand.Intn(len(shuffled))
						shuffled = append(shuffled, shuffled[i])
						shuffled[i] = f
					}
				}
			}

			// queue shuffled files
			for _, f := range shuffled {
				nextFile <- f
				if s.Verbose {
					fmt.Printf("Next: %v\n", f)
				}
			}
		}
	}()

	// decodeFile decodes one audio file and sends its PCM chunks to nextFrame
	// with appropriate pacing. It is called from separate goroutines below.
	decodeFile := func(filename string, cumwait *time.Duration) {
		dec := decoderForFile(filename)
		if dec == nil {
			return
		}
		f, err := os.Open(filename)
		if err != nil {
			if s.Debug {
				log.Printf("Skipped %q: %v", filename, err)
			}
			return
		}
		defer f.Close()

		samples, rate, ch, err := dec.Decode(f)
		if err != nil {
			if s.Debug {
				log.Printf("Skipped %q: decode error: %v", filename, err)
			}
			return
		}
		if s.Verbose {
			fmt.Printf("Now playing: %v\n", filename)
		}

		frames := chunk(normalise(samples, rate, ch), 8820)
		for _, frame := range frames {
			t0 := time.Now()
			frameBytes := int16sToBytes(frame)
			nextFrame <- frameBytes
			// frame duration = samples / (channels * sample_rate)
			towait := time.Duration(len(frameBytes))*time.Second/(2*2*canonRate) - time.Since(t0)
			*cumwait += towait
			if *cumwait > time.Second {
				time.Sleep(*cumwait)
				*cumwait = 0
			}
		}
	}

	// stdin path: decode a single MP3 stream piped to standard input.
	go func() {
		if path != "-" {
			return
		}
		dec := decoderForFile(".mp3") // stdin is assumed to be MP3
		if dec == nil {
			return
		}
		samples, rate, ch, err := dec.Decode(os.Stdin)
		if err != nil {
			log.Printf("stdin decode error: %v", err)
			return
		}
		var cumwait time.Duration
		frames := chunk(normalise(samples, rate, ch), 8820)
		for _, frame := range frames {
			t0 := time.Now()
			frameBytes := int16sToBytes(frame)
			nextFrame <- frameBytes
			towait := time.Duration(len(frameBytes))*time.Second/(2*2*canonRate) - time.Since(t0)
			cumwait += towait
			if cumwait > time.Second {
				time.Sleep(cumwait)
				cumwait = 0
			}
		}
	}()

	// file path: open, decode, normalise, and pace each queued audio file.
	go func() {
		if path == "-" {
			return
		}
		var cumwait time.Duration
		for {
			filename := <-nextFile
			decodeFile(filename, &cumwait)
		}
	}()

	// broadcast frame to clients
	go func() {
		for {
			f := <-nextFrame
			// notify clients of new audio frame or let them quit
			m.Lock()
			for _, ch := range m.clients {
				m.Unlock()
				ch <- f
				br := <-m.result // handle quitting clients
				if br.err != nil {
					m.Lock()
					close(m.clients[br.qid])
					delete(m.clients, br.qid)
					nclients := len(m.clients)
					m.Unlock()
					if s.Debug {
						log.Printf("Connection exited, qid: %v, error %v. Now streaming to %v connections.", br.qid, br.err, nclients)
					} else if s.Verbose {
						fmt.Printf("Connection exited, qid: %v. Now streaming to %v connections, at %v\n", br.qid, nclients, time.Now().Format(time.Stamp))
					}
				}
				m.Lock()
			}
			m.Unlock()
		}
	}()

	return m
}
