package main

import (
	"log"
	"math/rand"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Random sleep between 100ms and 1000ms
		sleepDuration := time.Duration(100+rand.Intn(901)) * time.Millisecond
		start := time.Now()
		time.Sleep(sleepDuration)
		actualDuration := time.Since(start)

		if actualDuration > sleepDuration+500*time.Millisecond {
			log.Printf("Warning: Sleep skew detected. Expected %v, but slept %v", sleepDuration, actualDuration)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Println("Mock server started on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
