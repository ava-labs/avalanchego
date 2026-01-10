// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// FIX: Test concurrent cache access to detect race conditions
func TestRPCCache_ConcurrentAccess(t *testing.T) {
	require := require.New(t)

	config := RPCCacheConfig{
		Enabled:  true,
		Size:     1000,
		TTL:      1 * time.Second,
		Readonly: true,
	}

	cache, err := newRPCCache(
		logging.NoLog{},
		config,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.NotNil(cache)
	defer cache.Shutdown()

	// Handler that returns different responses based on param
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []int  `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"result":%d}`, req.Params[0])
	})

	middleware := cache.Middleware(handler)

	// Test concurrent reads and writes
	const numGoroutines = 100
	const requestsPerGoroutine = 10

	var wg sync.WaitGroup
	var hits, misses atomic.Int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				// Use same param to test cache hits
				param := j % 5
				body := fmt.Sprintf(`{"method":"eth_getBalance","params":[%d]}`, param)

				req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
				w := httptest.NewRecorder()

				middleware.ServeHTTP(w, req)

				if w.Header().Get("X-Cache") == "HIT" {
					hits.Add(1)
				} else {
					misses.Add(1)
				}

				require.Equal(http.StatusOK, w.Code)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Hits: %d, Misses: %d", hits.Load(), misses.Load())

	// We should have many more hits than misses since we're using only 5 unique params
	require.Greater(hits.Load(), misses.Load(), "Should have more cache hits than misses")
}

// FIX: Test concurrent TTL expiration to detect race conditions
func TestRPCCache_ConcurrentTTLExpiration(t *testing.T) {
	require := require.New(t)

	config := RPCCacheConfig{
		Enabled:  true,
		Size:     100,
		TTL:      100 * time.Millisecond, // Short TTL for testing
		Readonly: true,
	}

	cache, err := newRPCCache(
		logging.NoLog{},
		config,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.NotNil(cache)
	defer cache.Shutdown()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	})

	middleware := cache.Middleware(handler)

	// Populate cache
	body := `{"method":"eth_getBalance","params":[1]}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)
	require.Equal("MISS", w.Header().Get("X-Cache"))

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Hammer the expired entry with concurrent requests
	const numGoroutines = 50
	var wg sync.WaitGroup
	errors := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
			w := httptest.NewRecorder()

			// Should not panic or cause race condition
			middleware.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				errors[id] = fmt.Errorf("unexpected status code: %d", w.Code)
			}
		}(i)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		require.NoError(err, "Goroutine %d failed", i)
	}
}

// FIX: Test cache invalidation during concurrent reads
func TestRPCCache_InvalidateDuringRead(t *testing.T) {
	require := require.New(t)

	config := RPCCacheConfig{
		Enabled:  true,
		Size:     1000,
		TTL:      1 * time.Minute,
		Readonly: true,
	}

	cache, err := newRPCCache(
		logging.NoLog{},
		config,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.NotNil(cache)
	defer cache.Shutdown()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	})

	middleware := cache.Middleware(handler)

	// Populate cache with multiple entries
	for i := 0; i < 10; i++ {
		body := fmt.Sprintf(`{"method":"eth_getBalance","params":[%d]}`, i)
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)
	}

	// Concurrent reads while invalidating
	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	// Reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for {
				select {
				case <-stopCh:
					return
				default:
					body := fmt.Sprintf(`{"method":"eth_getBalance","params":[%d]}`, id)
					req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
					w := httptest.NewRecorder()
					middleware.ServeHTTP(w, req)
				}
			}
		}(i)
	}

	// Invalidator goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)

		// This should not cause panic or race condition
		cache.Invalidate("")

		time.Sleep(50 * time.Millisecond)
		close(stopCh)
	}()

	wg.Wait()

	// Cache should be empty after invalidation
	require.Equal(0, cache.cache.Len())
}

// FIX: Test request coalescing (singleflight)
func TestRPCCache_RequestCoalescing(t *testing.T) {
	require := require.New(t)

	config := RPCCacheConfig{
		Enabled:  true,
		Size:     1000,
		TTL:      1 * time.Minute,
		Readonly: true,
	}

	cache, err := newRPCCache(
		logging.NoLog{},
		config,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.NotNil(cache)
	defer cache.Shutdown()

	var handlerCallCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCallCount.Add(1)
		// Simulate slow backend
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	})

	middleware := cache.Middleware(handler)

	// Send 100 identical requests concurrently
	const numRequests = 100
	body := `{"method":"eth_getBalance","params":[1]}`

	var wg sync.WaitGroup
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			require.Equal(http.StatusOK, w.Code)
		}()
	}

	wg.Wait()

	// With singleflight, handler should be called very few times (ideally 1)
	// Without it, would be called ~100 times
	calls := handlerCallCount.Load()
	t.Logf("Handler called %d times for %d concurrent requests", calls, numRequests)

	// Should be significantly fewer calls than total requests
	require.Less(calls, int32(10), "Singleflight should coalesce requests")
}

// FIX: Test memory safety - responseRecorder concurrent writes
func TestResponseRecorder_ConcurrentWrites(t *testing.T) {
	require := require.New(t)

	// Test with malicious handler that spawns goroutines
	config := RPCCacheConfig{
		Enabled:  true,
		Size:     100,
		TTL:      1 * time.Minute,
		Readonly: true,
	}

	cache, err := newRPCCache(
		logging.NoLog{},
		config,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	defer cache.Shutdown()

	// Handler that writes from multiple goroutines (bad practice but possible)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wg sync.WaitGroup

		// Spawn 10 goroutines that write concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				fmt.Fprintf(w, `{"chunk":%d}`, n)
			}(i)
		}

		wg.Wait()
	})

	middleware := cache.Middleware(handler)

	// This should not cause panic or race condition
	body := `{"method":"eth_getBalance","params":[1]}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	// Should not panic
	require.NotPanics(func() {
		middleware.ServeHTTP(w, req)
	})
}

// FIX: Test cache under heavy memory pressure
func TestRPCCache_MemoryLimit(t *testing.T) {
	require := require.New(t)

	config := RPCCacheConfig{
		Enabled:   true,
		Size:      10000,
		TTL:       1 * time.Minute,
		Readonly:  true,
		MaxMemory: 1 * 1024 * 1024, // 1MB limit
	}

	cache, err := newRPCCache(
		logging.NoLog{},
		config,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	defer cache.Shutdown()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return large response (100KB)
		w.WriteHeader(http.StatusOK)
		w.Write(bytes.Repeat([]byte("x"), 100*1024))
	})

	middleware := cache.Middleware(handler)

	// Try to cache 20 large responses (20 * 100KB = 2MB)
	// Only ~10 should fit within 1MB limit
	for i := 0; i < 20; i++ {
		body := fmt.Sprintf(`{"method":"eth_getBalance","params":[%d]}`, i)
		req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)
	}

	// Cache should respect memory limit
	t.Logf("Cache size: %d entries, Memory: %d bytes",
		cache.cache.Len(), cache.currentMemory)

	require.LessOrEqual(cache.currentMemory, config.MaxMemory,
		"Cache should respect memory limit")
}

// FIX: Test non-200 status codes are not cached
func TestRPCCache_NonSuccessStatusCodes(t *testing.T) {
	require := require.New(t)

	config := DefaultRPCCacheConfig()
	cache, err := newRPCCache(
		logging.NoLog{},
		config,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	defer cache.Shutdown()

	testCases := []struct {
		name       string
		statusCode int
		shouldCache bool
	}{
		{"200 OK", http.StatusOK, true},
		{"201 Created", http.StatusCreated, false},
		{"400 Bad Request", http.StatusBadRequest, false},
		{"404 Not Found", http.StatusNotFound, false},
		{"500 Internal Server Error", http.StatusInternalServerError, false},
		{"503 Service Unavailable", http.StatusServiceUnavailable, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				w.Write([]byte(`{"result":"ok"}`))
			})

			middleware := cache.Middleware(handler)

			// First request
			body := fmt.Sprintf(`{"method":"eth_getBalance","params":[%d]}`, tc.statusCode)
			req1 := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
			w1 := httptest.NewRecorder()
			middleware.ServeHTTP(w1, req1)
			require.Equal("MISS", w1.Header().Get("X-Cache"))

			// Second request - should be HIT only if status was 200
			req2 := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
			w2 := httptest.NewRecorder()
			middleware.ServeHTTP(w2, req2)

			if tc.shouldCache {
				require.Equal("HIT", w2.Header().Get("X-Cache"),
					"Status %d should be cached", tc.statusCode)
			} else {
				require.Equal("MISS", w2.Header().Get("X-Cache"),
					"Status %d should NOT be cached", tc.statusCode)
			}
		})
	}
}

// FIX: Test context cancellation
func TestRPCCache_ContextCancellation(t *testing.T) {
	require := require.New(t)

	config := DefaultRPCCacheConfig()
	cache, err := newRPCCache(
		logging.NoLog{},
		config,
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	defer cache.Shutdown()

	handlerCalled := atomic.Bool{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled.Store(true)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	})

	middleware := cache.Middleware(handler)

	// Create request with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	body := `{"method":"eth_getBalance","params":[1]}`
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewBufferString(body))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	// Handler should not be called for cancelled request
	require.False(handlerCalled.Load(),
		"Handler should not be called when context is cancelled")
}
