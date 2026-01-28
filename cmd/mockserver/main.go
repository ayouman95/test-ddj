package main

import (
	"flag"
	"log"
	"math/rand"
	"net/http"
	"time"
)

func main() {
	var sleepFlag time.Duration
	flag.DurationVar(&sleepFlag, "sleep", -1, "Fixed sleep duration. Set to 0 for stress test. Default -1 (random 100-1000ms)")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if sleepFlag >= 0 {
			if sleepFlag > 0 {
				time.Sleep(sleepFlag)
			}
		} else {
			// Random sleep between 100ms and 1000ms
			sleepDuration := time.Duration(100+rand.Intn(901)) * time.Millisecond
			time.Sleep(sleepDuration)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Println("Mock server started on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
