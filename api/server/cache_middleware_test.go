// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestRPCCache_CacheableMethod(t *testing.T) {
	require := require.New(t)

	// Create cache
	cache, err := newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled:  true,
			Size:     100,
			TTL:      1 * time.Minute,
			Readonly: true,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.NotNil(cache)

	// Create test handler
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":"0x123","id":1}`))
	})

	// Wrap with cache middleware
	cachedHandler := cache.Middleware(handler)

	// Make first request (cache MISS)
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x0000000000000000000000000000000000000000","latest"],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)

	require.Equal(http.StatusOK, rec1.Code)
	require.Equal(1, callCount, "First request should call handler")
	require.Equal("MISS", rec1.Header().Get("X-Cache"))

	// Make second identical request (cache HIT)
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x0000000000000000000000000000000000000000","latest"],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(http.StatusOK, rec2.Code)
	require.Equal(1, callCount, "Second request should NOT call handler (cached)")
	require.Equal("HIT", rec2.Header().Get("X-Cache"))
	require.Equal(rec1.Body.String(), rec2.Body.String(), "Cached response should match original")
}

func TestRPCCache_NonCacheableMethod(t *testing.T) {
	require := require.New(t)

	cache, err := newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled:  true,
			Size:     100,
			TTL:      1 * time.Minute,
			Readonly: true,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":"0xabc","id":1}`))
	})

	cachedHandler := cache.Middleware(handler)

	// Make request with non-cacheable method (sendTransaction)
	req := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_sendTransaction","params":[{}],"id":1}`))
	rec := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec, req)

	require.Equal(http.StatusOK, rec.Code)
	require.Equal(1, callCount, "Should call handler for non-cacheable method")

	// Second request should also call handler
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_sendTransaction","params":[{}],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(2, callCount, "Should call handler again for non-cacheable method")
}

func TestRPCCache_TTLExpiration(t *testing.T) {
	require := require.New(t)

	cache, err := newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled:  true,
			Size:     100,
			TTL:      100 * time.Millisecond, // Short TTL for testing
			Readonly: true,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":"0x123","id":1}`))
	})

	cachedHandler := cache.Middleware(handler)

	// First request
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)
	require.Equal(1, callCount)

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Second request after TTL expiration
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(2, callCount, "Should call handler after TTL expiration")
}

func TestRPCCache_DifferentParams(t *testing.T) {
	require := require.New(t)

	cache, err := newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled:  true,
			Size:     100,
			TTL:      1 * time.Minute,
			Readonly: true,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write(body) // Echo back the request
	})

	cachedHandler := cache.Middleware(handler)

	// Request 1
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x1111111111111111111111111111111111111111","latest"],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)

	// Request 2 with different params
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x2222222222222222222222222222222222222222","latest"],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(2, callCount, "Different params should result in different cache entries")
}

func TestRPCCache_Invalidate(t *testing.T) {
	require := require.New(t)

	cache, err := newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled:  true,
			Size:     100,
			TTL:      1 * time.Minute,
			Readonly: true,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	})

	cachedHandler := cache.Middleware(handler)

	// First request (cache MISS)
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)
	require.Equal(1, callCount)

	// Invalidate cache
	cache.Invalidate("")

	// Second request after invalidation (cache MISS again)
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(2, callCount, "Should call handler after cache invalidation")
}

func TestRPCCache_Disabled(t *testing.T) {
	require := require.New(t)

	cache, err := newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled: false, // Cache disabled
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)
	require.Nil(cache, "Disabled cache should return nil")

	// Middleware should be pass-through
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})

	cachedHandler := cache.Middleware(handler)

	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(2, callCount, "Should call handler for both requests when cache is disabled")
}

func TestRPCCache_GETRequestNotCached(t *testing.T) {
	require := require.New(t)

	cache, err := newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled:  true,
			Size:     100,
			TTL:      1 * time.Minute,
			Readonly: true,
		},
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})

	cachedHandler := cache.Middleware(handler)

	// GET requests should not be cached
	req1 := httptest.NewRequest(http.MethodGet, "/ext/bc/C/rpc", nil)
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/ext/bc/C/rpc", nil)
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(2, callCount, "GET requests should not be cached")
}

func BenchmarkRPCCache_Hit(b *testing.B) {
	cache, _ := newRPCCache(
		logging.NoLog{},
		DefaultRPCCacheConfig(),
		prometheus.NewRegistry(),
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	})

	cachedHandler := cache.Middleware(handler)

	// Prime the cache
	req := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x0000000000000000000000000000000000000000","latest"],"id":1}`))
	rec := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec, req)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x0000000000000000000000000000000000000000","latest"],"id":1}`))
		rec := httptest.NewRecorder()
		cachedHandler.ServeHTTP(rec, req)
	}
}

func BenchmarkRPCCache_Miss(b *testing.B) {
	cache, _ := newRPCCache(
		logging.NoLog{},
		DefaultRPCCacheConfig(),
		prometheus.NewRegistry(),
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	})

	cachedHandler := cache.Middleware(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Each request has different params to force cache miss
		req := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x000000000000000000000000000000000000000`+string(rune(i))+`","latest"],"id":1}`))
		rec := httptest.NewRecorder()
		cachedHandler.ServeHTTP(rec, req)
	}
}
