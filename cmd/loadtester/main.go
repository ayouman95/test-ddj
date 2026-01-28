package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Stats struct {
	Requests      uint64
	Successes     uint64
	Failures      uint64
	TotalDuration int64 // in nanoseconds
}

func main() {
	var (
		urlsStr  string
		workers  int
		duration time.Duration
	)

	flag.StringVar(&urlsStr, "urls", "http://localhost:8080", "Comma-separated list of URLs to test")
	flag.IntVar(&workers, "workers", 100, "Number of concurrent workers")
	flag.DurationVar(&duration, "duration", 10*time.Second, "Duration to run the test")
	flag.Parse()

	urls := strings.Split(urlsStr, ",")
	if len(urls) == 0 {
		log.Fatal("No URLs provided")
	}

	fmt.Printf("Starting load test against %v with %d workers for %v\n", urls, workers, duration)

	var stats Stats
	var wg sync.WaitGroup

	done := make(chan struct{})

	// Timer to stop the test
	go func() {
		time.Sleep(duration)
		close(done)
	}()

	// Ticker for reporting stats every second
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		var lastReqs uint64
		var lastDuration int64
		for {
			select {
			case <-ticker.C:
				currReqs := atomic.LoadUint64(&stats.Requests)
				currDuration := atomic.LoadInt64(&stats.TotalDuration)

				diffReqs := currReqs - lastReqs
				diffDuration := currDuration - lastDuration

				avgLatency := time.Duration(0)
				if diffReqs > 0 {
					avgLatency = time.Duration(diffDuration / int64(diffReqs))
				}

				fmt.Printf("RPS: %d, Avg Latency: %v\n", diffReqs, avgLatency)

				lastReqs = currReqs
				lastDuration = currDuration
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        workers,
			MaxIdleConnsPerHost: workers,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: 5 * time.Second,
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					url := urls[0] // Simple round-robin or random selection can be added if multiple URLs
					start := time.Now()
					resp, err := client.Get(url)
					elapsed := time.Since(start)

					atomic.AddUint64(&stats.Requests, 1)
					atomic.AddInt64(&stats.TotalDuration, int64(elapsed))

					if err != nil {
						atomic.AddUint64(&stats.Failures, 1)
					} else {
						if resp.StatusCode >= 200 && resp.StatusCode < 300 {
							atomic.AddUint64(&stats.Successes, 1)
						} else {
							atomic.AddUint64(&stats.Failures, 1)
						}
						resp.Body.Close()
					}
				}
			}
		}()
	}

	wg.Wait()

	totalReqs := atomic.LoadUint64(&stats.Requests)
	totalSuccess := atomic.LoadUint64(&stats.Successes)
	totalFailures := atomic.LoadUint64(&stats.Failures)
	totalDuration := atomic.LoadInt64(&stats.TotalDuration)

	avgLatency := time.Duration(0)
	if totalReqs > 0 {
		avgLatency = time.Duration(totalDuration / int64(totalReqs))
	}

	fmt.Println("\n--- Test Finished ---")
	fmt.Printf("Total Requests: %d\n", totalReqs)
	fmt.Printf("Successes: %d\n", totalSuccess)
	fmt.Printf("Failures: %d\n", totalFailures)
	fmt.Printf("Average Latency: %v\n", avgLatency)
	fmt.Printf("Overall RPS: %.2f\n", float64(totalReqs)/duration.Seconds())
}
