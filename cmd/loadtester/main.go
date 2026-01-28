package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// raiseLimit attempts to maximize the file descriptor limit for this process
func raiseLimit() {
	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		log.Printf("Error getting rlimit: %v", err)
		return
	}

	log.Printf("Current file descriptor limit: %d (cur) / %d (max)", rLimit.Cur, rLimit.Max)

	// Try to set Cur to Max
	rLimit.Cur = rLimit.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		log.Printf("Error raising rlimit to max: %v", err)
		// Fallback: try setting to a reasonable high number if Max is infinity or huge
		if rLimit.Max > 65535 {
			rLimit.Cur = 65535
			if err2 := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err2 == nil {
				log.Printf("Fallback: Raised limit to %d", rLimit.Cur)
			}
		}
	} else {
		log.Printf("Successfully raised file descriptor limit to %d", rLimit.Cur)
	}
}

type Stats struct {
	Requests      uint64
	Successes     uint64
	Failures      uint64
	TotalDuration int64 // in nanoseconds

	ErrorMu     sync.Mutex
	ErrorCounts map[string]uint64
}

func main() {
	raiseLimit()

	var (
		urlsStr  string
		workers  int
		duration time.Duration
		rampUp   time.Duration
		timeout  time.Duration
	)

	flag.StringVar(&urlsStr, "urls", "http://localhost:8080", "Comma-separated list of URLs to test")
	flag.IntVar(&workers, "workers", 100, "Number of concurrent workers")
	flag.DurationVar(&duration, "duration", 10*time.Second, "Duration to run the test")
	flag.DurationVar(&rampUp, "rampup", 0, "Ramp up duration to stagger worker start")
	flag.DurationVar(&timeout, "timeout", 2*time.Second, "Response header timeout")
	flag.Parse()

	urls := strings.Split(urlsStr, ",")
	if len(urls) == 0 {
		log.Fatal("No URLs provided")
	}

	fmt.Printf("Starting load test against %v with %d workers for %v (rampup: %v, timeout: %v)\n", urls, workers, duration, rampUp, timeout)

	stats := Stats{
		ErrorCounts: make(map[string]uint64),
	}
	var wg sync.WaitGroup

	done := make(chan struct{})

	// Timer to stop the test
	go func() {
		// Total duration includes rampUp time to ensure full load runs for 'duration'
		// actually user usually expects 'duration' to correspond to the load test window.
		// but if we stagger, the last worker starts at T+rampUp.
		// Let's simply run for duration + rampUp so the last worker gets chance to run.
		// Or better: keep 'duration' as the total runtime from start. User should set duration > rampUp.
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

	// Custom Transport to optimize connection handling and separate timeouts
	client := &http.Client{
		Transport: &http.Transport{
			// DialContext handles the connection establishment (including DNS, TCP handshake, and waiting for local resources/ports).
			// We give it a generous timeout (30s) so that client-side queuing/busy-ness doesn't cause a premature failure.
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			// ResponseHeaderTimeout is the time to wait for the server's response headers AFTER the connection is established and request is sent.
			// This specifically targets the "server processing time" (plus network round trip), which is what you want to test.
			// Actually, let's strictly use a new variable or hardcode meaningful defaults.
			// User asked: "can I only control server return time".
			// Let's set ResponseHeaderTimeout to 2s (server limit) and Dial to 30s (queue limit).
			ResponseHeaderTimeout: timeout,
			MaxIdleConns:          workers,
			MaxIdleConnsPerHost:   workers,
			IdleConnTimeout:       90 * time.Second,
		},
		Timeout: 0, // Disable end-to-end timeout to avoid penalizing queuing time
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)

		// Calculate delay for this worker
		startDelay := time.Duration(0)
		if rampUp > 0 {
			startDelay = time.Duration(float64(rampUp) * float64(i) / float64(workers))
		}

		go func(delay time.Duration) {
			defer wg.Done()

			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-done:
					return
				}
			}

			for {
				select {
				case <-done:
					return
				default:
					url := urls[0]
					req, _ := http.NewRequest("GET", url, nil)
					req.Header.Set("X-Req-Start", fmt.Sprintf("%d", time.Now().UnixMicro()))

					start := time.Now()
					resp, err := client.Do(req)
					elapsed := time.Since(start)

					atomic.AddUint64(&stats.Requests, 1)
					atomic.AddInt64(&stats.TotalDuration, int64(elapsed))

					if err != nil {
						atomic.AddUint64(&stats.Failures, 1)
						stats.ErrorMu.Lock()
						stats.ErrorCounts[err.Error()]++
						stats.ErrorMu.Unlock()
					} else {
						if resp.StatusCode >= 200 && resp.StatusCode < 300 {
							atomic.AddUint64(&stats.Successes, 1)
						} else {
							atomic.AddUint64(&stats.Failures, 1)
							statusMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
							stats.ErrorMu.Lock()
							stats.ErrorCounts[statusMsg]++
							stats.ErrorMu.Unlock()
						}
						resp.Body.Close()
					}
				}
			}
		}(startDelay)
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

	if totalFailures > 0 {
		fmt.Println("\nError Details:")
		stats.ErrorMu.Lock()
		for errStr, count := range stats.ErrorCounts {
			fmt.Printf("- %s: %d\n", errStr, count)
		}
		stats.ErrorMu.Unlock()
	}
}
