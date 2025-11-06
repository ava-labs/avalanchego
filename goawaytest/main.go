package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/proposervm"
)

func main() {
	const numThreads = 100
	const reqsPerThread = 100
	const targetURL = "https://api.avax-test.network/ext/bc/C/proposervm"

	// Check for optional token parameter
	var token string
	if len(os.Args) > 1 {
		token = os.Args[1]
		fmt.Printf("Using token: %s\n", token)
	} else {
		fmt.Printf("No token provided\n")
	}

	fmt.Printf("Starting load test with %d threads, %d requests per thread\n", numThreads, reqsPerThread)
	fmt.Printf("Target URL: %s\n", targetURL)
	fmt.Printf("Total requests: %d\n", numThreads*reqsPerThread)

	var totalErrors int64
	var goawayErrors int64

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		go func(threadID int) {
			defer wg.Done()
			runThread(threadID, targetURL, token, reqsPerThread, &totalErrors, &goawayErrors)
		}(i)
	}

	wg.Wait()

	duration := time.Since(startTime)
	totalRequests := numThreads * reqsPerThread
	fmt.Printf("Completed %d requests in %v (%.2f req/sec)\n",
		totalRequests, duration, float64(totalRequests)/duration.Seconds())

	fmt.Printf("Total errors: %d\n", atomic.LoadInt64(&totalErrors))
	fmt.Printf("GOAWAY errors: %d\n", atomic.LoadInt64(&goawayErrors))
}

func runThread(threadID int, targetURL string, token string, reqsPerThread int, totalErrors *int64, goawayErrors *int64) {
	client := &proposervm.JSONRPCClient{
		Requester: rpc.NewEndpointRequester(targetURL),
	}

	ctx := context.Background()

	for i := 0; i < reqsPerThread; i++ {
		var err error
		if token != "" {
			_, err = client.GetCurrentEpoch(ctx, rpc.WithQueryParam("token", token))
		} else {
			_, err = client.GetCurrentEpoch(ctx)
		}

		if err != nil {
			atomic.AddInt64(totalErrors, 1)

			errorStr := err.Error()
			if strings.Contains(strings.ToUpper(errorStr), "GOAWAY") {
				atomic.AddInt64(goawayErrors, 1)
			}

			log.Printf("thread %d request %d errored: %v", threadID, i, err)
		}
	}
}
