// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// DefaultCacheSize is the default number of cached RPC responses
	DefaultCacheSize = 10000

	// DefaultCacheTTL is the default time-to-live for cache entries
	DefaultCacheTTL = 5 * time.Minute

	// MaxResponseSize is the maximum size of a response to cache (10MB)
	MaxResponseSize = 10 * 1024 * 1024
)

// RPCCacheConfig contains configuration for the RPC cache
type RPCCacheConfig struct {
	Enabled  bool
	Size     int
	TTL      time.Duration
	Readonly bool // Only cache read-only methods
}

// DefaultRPCCacheConfig returns the default cache configuration
func DefaultRPCCacheConfig() RPCCacheConfig {
	return RPCCacheConfig{
		Enabled:  true,
		Size:     DefaultCacheSize,
		TTL:      DefaultCacheTTL,
		Readonly: true,
	}
}

type cacheEntry struct {
	response  []byte
	headers   http.Header
	timestamp time.Time
	status    int
}

type cacheMetrics struct {
	hits      prometheus.Counter
	misses    prometheus.Counter
	evictions prometheus.Counter
	size      prometheus.Gauge
}

// rpcCache implements RPC response caching with TTL
type rpcCache struct {
	log     logging.Logger
	config  RPCCacheConfig
	cache   *lru.Cache[string, *cacheEntry]
	metrics *cacheMetrics
	mu      sync.RWMutex

	// Read-only methods that are safe to cache
	cacheableMethods map[string]bool
}

// newRPCCache creates a new RPC cache instance
func newRPCCache(log logging.Logger, config RPCCacheConfig, registerer prometheus.Registerer) (*rpcCache, error) {
	if !config.Enabled {
		return nil, nil
	}

	// Validate config (BUG #10 fix)
	if config.Size <= 0 {
		return nil, fmt.Errorf("cache size must be positive, got %d", config.Size)
	}
	if config.TTL <= 0 {
		return nil, fmt.Errorf("cache TTL must be positive, got %v", config.TTL)
	}

	metrics := &cacheMetrics{
		hits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "rpc_cache_hits",
			Help: "Number of RPC cache hits",
		}),
		misses: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "rpc_cache_misses",
			Help: "Number of RPC cache misses",
		}),
		evictions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "rpc_cache_evictions",
			Help: "Number of RPC cache evictions",
		}),
		size: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "rpc_cache_size",
			Help: "Current number of entries in RPC cache",
		}),
	}

	// Register metrics with cleanup on failure (BUG #12 fix)
	if err := registerer.Register(metrics.hits); err != nil {
		return nil, err
	}
	if err := registerer.Register(metrics.misses); err != nil {
		registerer.Unregister(metrics.hits)
		return nil, err
	}
	if err := registerer.Register(metrics.evictions); err != nil {
		registerer.Unregister(metrics.hits)
		registerer.Unregister(metrics.misses)
		return nil, err
	}
	if err := registerer.Register(metrics.size); err != nil {
		registerer.Unregister(metrics.hits)
		registerer.Unregister(metrics.misses)
		registerer.Unregister(metrics.evictions)
		return nil, err
	}

	cache := lru.NewCacheWithOnEvict(config.Size, func(key string, value *cacheEntry) {
		metrics.evictions.Inc()
		metrics.size.Dec()
	})

	rc := &rpcCache{
		log:     log,
		config:  config,
		cache:   cache,
		metrics: metrics,
		cacheableMethods: map[string]bool{
			"eth_call":                   true,
			"eth_getBalance":             true,
			"eth_getCode":                true,
			"eth_getStorageAt":           true,
			"eth_getBlockByNumber":       true,
			"eth_getBlockByHash":         true,
			"eth_getTransactionByHash":   true,
			"eth_getTransactionReceipt":  true,
			"eth_getBlockTransactionCountByNumber": true,
			"eth_getBlockTransactionCountByHash":   true,
			"eth_getUncleCountByBlockNumber":       true,
			"eth_getUncleCountByBlockHash":         true,
			"eth_getTransactionCount":              true,
			"eth_getLogs":                          true,
			"net_version":                          true,
			"web3_clientVersion":                   true,
			"web3_sha3":                            true,
		},
	}

	// Set initial size metric (BUG #7 fix)
	rc.metrics.size.Set(float64(rc.cache.Len()))

	return rc, nil
}

// Middleware returns an HTTP middleware that caches responses
func (rc *rpcCache) Middleware(next http.Handler) http.Handler {
	if rc == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only cache POST requests (JSON-RPC)
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		// Read request body
		body, err := io.ReadAll(r.Body)
		// Always restore body (BUG #5 fix)
		r.Body = io.NopCloser(bytes.NewBuffer(body))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		// Detect batch requests (BUG #9 fix)
		if len(body) > 0 && body[0] == '[' {
			next.ServeHTTP(w, r)
			return
		}

		// Parse JSON-RPC request
		var rpcReq struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &rpcReq); err != nil {
			next.ServeHTTP(w, r)
			return
		}

		// Check if method is cacheable
		if !rc.isCacheable(rpcReq.Method) {
			next.ServeHTTP(w, r)
			return
		}

		// Normalize nil/empty params (BUG #13 and #19 fix)
		paramStr := string(rpcReq.Params)
		if len(rpcReq.Params) == 0 || paramStr == "null" || paramStr == "{}" {
			rpcReq.Params = json.RawMessage("[]")
		} else {
			// Canonicalize JSON to handle whitespace variations (BUG #18 fix)
			var params interface{}
			if err := json.Unmarshal(rpcReq.Params, &params); err == nil {
				if canonical, err := json.Marshal(params); err == nil {
					rpcReq.Params = canonical
				}
				// If marshal fails, use original params
			}
		}

		// Generate cache key
		cacheKey := rc.generateKey(rpcReq.Method, rpcReq.Params)

		// Try to get from cache
		if entry, found := rc.get(cacheKey); found {
			rc.metrics.hits.Inc()
			// Copy response bytes to prevent data races (BUG #15 fix)
			responseCopy := make([]byte, len(entry.response))
			copy(responseCopy, entry.response)

			// Restore cached headers (BUG #16 fix)
			for k, v := range entry.headers {
				w.Header()[k] = v
			}
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(entry.status)
			// Log write errors (BUG #6 fix)
			if _, err := w.Write(responseCopy); err != nil {
				rc.log.Error("failed to write cached response", zap.Error(err))
			}
			return
		}

		rc.metrics.misses.Inc()

		// Set X-Cache header before serving (BUG #3 fix)
		w.Header().Set("X-Cache", "MISS")

		// Capture response
		recorder := &responseRecorder{
			ResponseWriter: w,
			body:           new(bytes.Buffer),
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(recorder, r)

		// Cache successful responses within size limit
		responseBytes := recorder.body.Bytes()
		if recorder.statusCode == http.StatusOK {
			// Check max response size (BUG #8 fix)
			if len(responseBytes) <= MaxResponseSize {
				// Capture headers (BUG #16 fix)
				headers := make(http.Header)
				for k, v := range recorder.Header() {
					headers[k] = v
				}

				rc.put(cacheKey, &cacheEntry{
					response:  responseBytes,
					headers:   headers,
					timestamp: time.Now(),
					status:    recorder.statusCode,
				})
			} else {
				rc.log.Debug("response too large to cache",
					zap.String("method", rpcReq.Method),
					zap.Int("size", len(responseBytes)),
					zap.Int("maxSize", MaxResponseSize))
			}
		}
	})
}

func (rc *rpcCache) isCacheable(method string) bool {
	// Always check whitelist to prevent caching state-changing methods (BUG #11 fix)
	// Readonly flag just enables/disables caching entirely
	if !rc.config.Readonly {
		return false
	}
	return rc.cacheableMethods[method]
}

func (rc *rpcCache) generateKey(method string, params json.RawMessage) string {
	hash := sha256.New()
	hash.Write([]byte(method))
	hash.Write(params)
	return hex.EncodeToString(hash.Sum(nil))
}

func (rc *rpcCache) get(key string) (*cacheEntry, bool) {
	// BUG #1 fix: Remove defer to avoid deadlock with manual lock management
	rc.mu.RLock()

	entry, found := rc.cache.Get(key)
	if !found {
		rc.mu.RUnlock()
		return nil, false
	}

	// Check TTL
	if time.Since(entry.timestamp) > rc.config.TTL {
		rc.mu.RUnlock()

		// Upgrade to write lock for eviction
		rc.mu.Lock()
		// BUG #2 fix: Re-check after acquiring write lock
		entry, found = rc.cache.Get(key)
		if found && time.Since(entry.timestamp) > rc.config.TTL {
			rc.cache.Evict(key)
		}
		rc.mu.Unlock()
		return nil, false
	}

	rc.mu.RUnlock()
	return entry, true
}

func (rc *rpcCache) put(key string, entry *cacheEntry) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.cache.Put(key, entry)
	rc.metrics.size.Set(float64(rc.cache.Len()))
}

// Invalidate removes entries from the cache
func (rc *rpcCache) Invalidate(prefix string) {
	if rc == nil {
		return
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	// For now, flush entire cache on invalidation
	// TODO: Implement prefix-based invalidation
	rc.cache.Flush()
	rc.metrics.size.Set(0)
}

// responseRecorder captures the response for caching
type responseRecorder struct {
	http.ResponseWriter
	body          *bytes.Buffer
	statusCode    int
	headerWritten bool // BUG #4 fix: Track if WriteHeader was called
}

func (rec *responseRecorder) Write(buf []byte) (int, error) {
	// Auto-call WriteHeader if not called yet
	if !rec.headerWritten {
		rec.WriteHeader(http.StatusOK)
	}
	rec.body.Write(buf)
	return rec.ResponseWriter.Write(buf)
}

func (rec *responseRecorder) WriteHeader(statusCode int) {
	// BUG #4 fix: Only set status on first call
	if rec.headerWritten {
		return
	}
	rec.headerWritten = true
	rec.statusCode = statusCode
	rec.ResponseWriter.WriteHeader(statusCode)
}
