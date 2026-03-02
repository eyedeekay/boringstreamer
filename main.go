package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fgergo/boringstreamer/lib"
)

func main() {
	addr := flag.String("addr", ":4444", "listen on address (:port or host:port)")
	maxConnections := flag.Int("max", 42, "set maximum number of streaming connections")
	recursively := flag.Bool("r", true, "recursively look for music starting from path")
	verbose := flag.Bool("v", false, "display verbose messages")
	stdinFmt := flag.String("fmt", "mp3", "audio format read from stdin when path is \"-\" (mp3 or flac)")

	flag.Usage = func() {
		fmt.Printf("Usage: %s [flags] [path]\n", os.Args[0])
		fmt.Println("then browse to listen. (e.g. http://localhost:4444/)")
		fmt.Printf("%v does not follow links.\n", os.Args[0])
		fmt.Printf("To stream from standard input: %v [-fmt mp3|flac] -\n\n", os.Args[0])
		fmt.Println("flags:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Printf("Developer flag (positional, must follow path):\n")
		fmt.Printf("  -debug\tenable extra debug logging and pprof endpoint on :6060\n")
		fmt.Printf("  Example: %v /path/to/music -debug\n", os.Args[0])
	}
	flag.Parse()
	if len(flag.Args()) > 1 && flag.Args()[1] != "-debug" {
		flag.Usage()
		os.Exit(1)
	}

	path := "."
	debug := false
	switch len(flag.Args()) {
	case 0:
		if *verbose {
			fmt.Printf("Using path %#v, see -h for details.\n", path)
		}
	case 1:
		path = flag.Args()[0]
	case 2:
		path = flag.Args()[0]
		debug = true
	}

	// check if path is available
	if path != "-" {
		matches, err := filepath.Glob(path)
		if err != nil || len(matches) < 1 {
			fmt.Fprintf(os.Stderr, "Error: \"%v\" unavailable, nothing to play.\n", path)
			os.Exit(1)
		}

		err = os.Chdir(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: \"%v\" unavailable, nothing to play. Error: %v\n", path, err)
			os.Exit(1)
		}
		path, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: \"%v\" unavailable, nothing to play. Error: %v\n", path, err)
			os.Exit(1)
		}

		if *verbose {
			fmt.Printf("Looking for files available from \"%v\" ...\n", path)
		}
	}

	s := &lib.Streamer{
		MaxConnections: *maxConnections,
		Recursive:      *recursively,
		Verbose:        *verbose,
		Debug:          debug,
		Path:           path,
		StdinFormat:    *stdinFmt,
	}

	if err := s.ListenAndServe(*addr); err != nil {
		fmt.Fprintf(os.Stderr, "Exiting, error: %v\n", err) // log.Fatalf() race with log.SetPrefix()
		os.Exit(1)
	}
}
