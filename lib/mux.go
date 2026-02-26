// mux handles broadcasting audio streams to multiple subscribed clients.
package lib

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fgergo/mp3" // this fork was only created to have a modularized version of boringstreamer. Original: github.com/tcolgate/mp3
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

	// flow structure: fs -> nextFile -> nextStream -> nextFrame -> subscribed http servers -> browsers
	nextFile := make(chan string)       // next file to be broadcast
	nextStream := make(chan io.Reader)  // next raw audio stream
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
				probably := strings.HasSuffix(strings.ToLower(info.Name()), ".mp3") // probably mp3
				if !info.IsDir() && !probably {
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

	// open file
	go func() {
		if path == "-" {
			nextStream <- os.Stdin
			return
		}

		for {
			filename := <-nextFile
			f, err := os.Open(filename)
			if err != nil {
				if s.Debug {
					log.Printf("Skipped \"%v\", err=%v", filename, err)
				}
				continue
			}
			nextStream <- bufio.NewReaderSize(f, 1024*1024)
			if s.Verbose {
				fmt.Printf("Now playing: %v\n", filename)
			}
		}
	}()

	// decode stream to frames and delay for frame duration
	go func() {
		skipped := 0
		nullwriter := new(nullWriter)
		var cumwait time.Duration
		for {
			streamReader := <-nextStream
			d := mp3.NewDecoder(streamReader)
			var f mp3.Frame
			for {
				t0 := time.Now()
				tmp := log.Prefix()
				if !s.Debug {
					log.SetOutput(nullwriter) // hack to silence mp3 debug/log output
				} else {
					log.SetPrefix("info: mp3 decode msg: ")
				}
				err := d.Decode(&f, &skipped)
				log.SetPrefix(tmp)
				if !s.Debug {
					log.SetOutput(os.Stderr)
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					if s.Debug {
						log.Printf("Skipping frame, d.Decode() err=%v", err)
					}
					continue
				}
				buf, err := ioutil.ReadAll(f.Reader())
				if err != nil {
					if s.Debug {
						log.Printf("Skipping frame, ioutil.ReadAll() err=%v", err)
					}
					continue
				}
				nextFrame <- buf

				towait := f.Duration() - time.Now().Sub(t0)
				cumwait += towait // towait can be negative -> cumwait
				if cumwait > 1*time.Second {
					time.Sleep(cumwait)
					cumwait = 0
				}
			}
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
