package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/paperos-labs/logapi"
	"github.com/paperos-labs/logapi/csvpass"
)

var (
	tsvFile    = "credentials.tsv"
	staleAfter = 93 * 24 * time.Hour
)

func main() {
	bind := flag.String("bind", "", "Address to bind on")
	port := flag.Int("port", 8080, "Port to listen on")
	compress := flag.String("compress", "zst", "Compression format (zst, bz2, gz, xz)")
	storageDir := flag.String("storage", "", "Storage dir")
	flag.StringVar(&tsvFile, "tsv", tsvFile, "Credentials file to use")
	flag.Parse()

	f, err := os.Open(tsvFile)
	if err != nil {
		f, err = os.Create(tsvFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening/creating CSV: %v\n", err)
			os.Exit(1)
		}
	}
	defer func() { _ = f.Close() }()

	auth, err := csvpass.Load(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading CSV: %v\n", err)
		os.Exit(1)
	}

	if len(*storageDir) == 0 {
		fmt.Fprintf(os.Stderr, "--storage is required\n")
		os.Exit(1)
	}
	if _, err := os.ReadDir(*storageDir); err != nil {
		fmt.Fprintf(os.Stderr, "%q cannot be read\n", *storageDir)
		os.Exit(1)
	}

	server, err := logapi.New(auth, *storageDir, *compress)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize server: %v\n", err)
		os.Exit(1)
	}

	tarballs, err := server.CompressAll(time.Now(), staleAfter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize server: %v\n", err)
		os.Exit(1)
	}
	for _, tarball := range tarballs {
		fmt.Printf("Compressed %s\n", tarball)
	}
	scheduleCompression(server, staleAfter)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/logs", server.UploadLog)
	mux.HandleFunc("GET /api/logs/{user}", server.ListMonths)
	mux.HandleFunc("GET /api/logs/{user}/{date}", server.ListFiles)
	mux.HandleFunc("GET /api/logs/{user}/{date}/{name}", server.GetFile)

	addr := fmt.Sprintf("%s:%d", *bind, *port)
	fmt.Fprintf(os.Stderr, "Listening on %s\n", addr)
	fmt.Fprintf(os.Stderr, "   POST /api/logs\n")
	fmt.Fprintf(os.Stderr, "   GET  /api/logs/{user}/{date}\n")
	fmt.Fprintf(os.Stderr, "   GET  /api/logs/{user}/{date}/{name}\n")
	log.Fatal(http.ListenAndServe(addr, mux))
}

// scheduleCompression runs compression for old folders
func scheduleCompression(server *logapi.Server, staleAfter time.Duration) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			now := time.Now()
			if now.Day() == 15 && now.Hour() == 3 && now.Minute() == 0 {
				tarballs, err := server.CompressAll(now, staleAfter)
				if err != nil {
					fmt.Fprintf(os.Stderr, "schedule error: %s", err)
					continue
				}
				for _, tarball := range tarballs {
					log.Printf("Compressed %s", tarball)
				}
			}
		}
	}()
}
