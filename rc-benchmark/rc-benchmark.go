package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

var (
	mutex = sync.Mutex{}
	times = []int64{}
)

func main() {
	h2server := &http2.Server{}
	h2server.MaxConcurrentStreams = 10000

	router := http.NewServeMux()

	router.Handle("GET /done", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(times) < 2 {
			fmt.Fprintf(w, "response")
			log.Println("Not enough data")
			return
		}
		sort.Slice(times, func(i, j int) bool {
			return times[i] < times[j]
		})
		prevTime := times[0]
		diffs := []int64{}
		for _, reqTime := range times[1:] {
			diffs = append(diffs, reqTime-prevTime)
			prevTime = reqTime
		}

		sort.Slice(diffs, func(i, j int) bool {
			return diffs[i] < diffs[j]
		})
		totalDiff := int64(0)
		diffStr := []string{}
		for _, diff := range diffs {
			totalDiff += diff
			diffStr = append(diffStr, strconv.FormatInt(diff, 10))
		}
		log.Println(strings.Join(diffStr, ","))
		log.Printf("Total requests: %d\n", len(times))
		log.Printf("Total time: %dns\n", totalDiff)
		log.Printf("Mean: %dns\n", totalDiff/int64(len(diffs)))
		log.Printf("Max: %dns\n", diffs[len(diffs)-1])
		log.Printf("Mediam: %dns\n", diffs[len(diffs)/2])
		log.Printf("Min: %dns\n", diffs[0])
		fmt.Fprintf(w, "response")
		log.Println("Done")
	}))

	router.Handle("POST /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println(err)
			fmt.Fprintf(w, "Err")
			return
		}
		log.Printf("Body: %s\n", body)
		mutex.Lock()
		times = append(times, time.Now().UnixNano())
		mutex.Unlock()
		fmt.Fprintf(w, "response")
	}))

	server := &http.Server{
		Addr:    "0.0.0.0:8080",
		Handler: h2c.NewHandler(router, h2server),
	}

	err := http2.ConfigureServer(server, h2server)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Listening on [0.0.0.0:8080]...\n")
	err = server.ListenAndServe()
	if err != nil {
		panic(err)
	}
}
