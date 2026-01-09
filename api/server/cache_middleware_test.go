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

// Test for BUG #9 fix: JSON-RPC batch requests not handled
func TestRPCCache_BatchRequestNotCached(t *testing.T) {
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
		w.Write([]byte(`[{"jsonrpc":"2.0","result":"0x1","id":1},{"jsonrpc":"2.0","result":"0x2","id":2}]`))
	})

	cachedHandler := cache.Middleware(handler)

	// Batch request (array)
	batchReq := `[{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1},{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":2}]`
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(batchReq))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(batchReq))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(2, callCount, "Batch requests should not be cached")
}

// Test for BUG #13 fix: Missing nil params handling
func TestRPCCache_NilParamsNormalization(t *testing.T) {
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
		w.Write([]byte(`{"jsonrpc":"2.0","result":"0x123","id":1}`))
	})

	cachedHandler := cache.Middleware(handler)

	// Request with params:null
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"net_version","params":null,"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)
	require.Equal(1, callCount)

	// Request with params omitted (should generate same cache key)
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"net_version","id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(1, callCount, "nil and missing params should generate same cache key")
	require.Equal("HIT", rec2.Header().Get("X-Cache"))
}

// Test for BUG #10 fix: Config validation
func TestRPCCache_ConfigValidation(t *testing.T) {
	require := require.New(t)

	// Test invalid size
	_, err := newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled:  true,
			Size:     0, // Invalid
			TTL:      1 * time.Minute,
			Readonly: true,
		},
		prometheus.NewRegistry(),
	)
	require.Error(err)
	require.Contains(err.Error(), "cache size must be positive")

	// Test invalid TTL
	_, err = newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled:  true,
			Size:     100,
			TTL:      0, // Invalid
			Readonly: true,
		},
		prometheus.NewRegistry(),
	)
	require.Error(err)
	require.Contains(err.Error(), "cache TTL must be positive")
}

// Test for BUG #11 fix: Readonly=false should disable caching
func TestRPCCache_ReadonlyFalseDisablesCaching(t *testing.T) {
	require := require.New(t)

	cache, err := newRPCCache(
		logging.NoLog{},
		RPCCacheConfig{
			Enabled:  true,
			Size:     100,
			TTL:      1 * time.Minute,
			Readonly: false, // Disabled
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
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x0000000000000000000000000000000000000000","latest"],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)

	// Second request (should NOT be cached when readonly=false)
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x0000000000000000000000000000000000000000","latest"],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(2, callCount, "Readonly=false should disable all caching")
}

// Test for BUG #8 fix: Max response size limit
func TestRPCCache_MaxResponseSize(t *testing.T) {
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

	// Create a large response (exceeds MaxResponseSize)
	largeResponse := make([]byte, MaxResponseSize+1)
	for i := range largeResponse {
		largeResponse[i] = 'x'
	}

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write(largeResponse)
	})

	cachedHandler := cache.Middleware(handler)

	// First request
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)

	// Second request (should NOT be cached due to size)
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(2, callCount, "Large responses should not be cached")
}

// Test for BUG #4 fix: WriteHeader called multiple times
func TestRPCCache_ResponseRecorderMultipleWriteHeader(t *testing.T) {
	require := require.New(t)

	recorder := &responseRecorder{
		ResponseWriter: httptest.NewRecorder(),
		body:           new(bytes.Buffer),
		statusCode:     http.StatusOK,
		headerWritten:  false,
	}

	// First call
	recorder.WriteHeader(http.StatusOK)
	require.Equal(http.StatusOK, recorder.statusCode)
	require.True(recorder.headerWritten)

	// Second call should be ignored
	recorder.WriteHeader(http.StatusInternalServerError)
	require.Equal(http.StatusOK, recorder.statusCode, "Second WriteHeader should be ignored")
}

// Test for BUG #16 fix: Response headers cached
func TestRPCCache_HeadersCached(t *testing.T) {
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
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "test-value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":"0x123","id":1}`))
	})

	cachedHandler := cache.Middleware(handler)

	// First request (cache MISS)
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x0000000000000000000000000000000000000000","latest"],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)

	require.Equal(1, callCount)
	require.Equal("MISS", rec1.Header().Get("X-Cache"))
	require.Equal("test-value", rec1.Header().Get("X-Custom-Header"))

	// Second request (cache HIT)
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x0000000000000000000000000000000000000000","latest"],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(1, callCount, "Second request should hit cache")
	require.Equal("HIT", rec2.Header().Get("X-Cache"))
	require.Equal("test-value", rec2.Header().Get("X-Custom-Header"), "Custom header should be cached")
	require.Equal("application/json", rec2.Header().Get("Content-Type"), "Content-Type should be cached")
}

// Test for BUG #18 fix: JSON whitespace normalization
func TestRPCCache_JSONWhitespaceNormalization(t *testing.T) {
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
		w.Write([]byte(`{"jsonrpc":"2.0","result":"0x123","id":1}`))
	})

	cachedHandler := cache.Middleware(handler)

	// Request with whitespace
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[1, 2, 3],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)
	require.Equal(1, callCount)

	// Request without whitespace (should hit same cache)
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[1,2,3],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(1, callCount, "Whitespace variations should hit same cache")
	require.Equal("HIT", rec2.Header().Get("X-Cache"))
}

// Test for BUG #19 fix: Empty object normalization
func TestRPCCache_EmptyObjectNormalization(t *testing.T) {
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
		w.Write([]byte(`{"jsonrpc":"2.0","result":"0x123","id":1}`))
	})

	cachedHandler := cache.Middleware(handler)

	// Request with empty object
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"net_version","params":{},"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)
	require.Equal(1, callCount)

	// Request with empty array (should hit same cache)
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"net_version","params":[],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	require.Equal(1, callCount, "Empty object and empty array should hit same cache")
	require.Equal("HIT", rec2.Header().Get("X-Cache"))

	// Request with null params (should also hit same cache)
	req3 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"net_version","params":null,"id":1}`))
	rec3 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec3, req3)

	require.Equal(1, callCount, "Null params should also hit same cache")
	require.Equal("HIT", rec3.Header().Get("X-Cache"))
}

// Test for BUG #20 and #21 fix: Headers deep-copied (no shared slices)
func TestRPCCache_HeadersIndependent(t *testing.T) {
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

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Multi", "value1")
		w.Header().Add("X-Multi", "value2")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	})

	cachedHandler := cache.Middleware(handler)

	// First request - cache MISS
	req1 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec1 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec1, req1)

	originalHeaders := rec1.Header()["X-Multi"]
	require.Equal([]string{"value1", "value2"}, originalHeaders)

	// Second request - cache HIT
	req2 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec2 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec2, req2)

	cachedHeaders := rec2.Header()["X-Multi"]
	require.Equal([]string{"value1", "value2"}, cachedHeaders)
	require.Equal("HIT", rec2.Header().Get("X-Cache"))

	// Verify headers are independent (no shared slices)
	// Modify the cached header
	if len(cachedHeaders) > 0 {
		cachedHeaders[0] = "MODIFIED"
	}

	// Third request - should still have original values
	req3 := httptest.NewRequest(http.MethodPost, "/ext/bc/C/rpc", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"eth_call","params":[],"id":1}`))
	rec3 := httptest.NewRecorder()
	cachedHandler.ServeHTTP(rec3, req3)

	// Should not be affected by modification above
	thirdHeaders := rec3.Header()["X-Multi"]
	require.Equal([]string{"value1", "value2"}, thirdHeaders, "Headers should be independent, not sharing slices")
}
