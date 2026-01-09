// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// DefaultCacheSize is the default number of cached RPC responses
	DefaultCacheSize = 10000

	// DefaultCacheTTL is the default time-to-live for cache entries
	DefaultCacheTTL = 5 * time.Minute
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

	if err := registerer.Register(metrics.hits); err != nil {
		return nil, err
	}
	if err := registerer.Register(metrics.misses); err != nil {
		return nil, err
	}
	if err := registerer.Register(metrics.evictions); err != nil {
		return nil, err
	}
	if err := registerer.Register(metrics.size); err != nil {
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
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(body))

		// Parse JSON-RPC request
		var rpcReq struct {
			Method string `json:"method"`
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

		// Generate cache key
		cacheKey := rc.generateKey(rpcReq.Method, rpcReq.Params)

		// Try to get from cache
		if entry, found := rc.get(cacheKey); found {
			rc.metrics.hits.Inc()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(entry.status)
			w.Write(entry.response)
			return
		}

		rc.metrics.misses.Inc()

		// Capture response
		recorder := &responseRecorder{
			ResponseWriter: w,
			body:           new(bytes.Buffer),
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(recorder, r)

		// Cache successful responses
		if recorder.statusCode == http.StatusOK {
			rc.put(cacheKey, &cacheEntry{
				response:  recorder.body.Bytes(),
				timestamp: time.Now(),
				status:    recorder.statusCode,
			})
		}

		// Set cache miss header
		recorder.Header().Set("X-Cache", "MISS")
	})
}

func (rc *rpcCache) isCacheable(method string) bool {
	if !rc.config.Readonly {
		return true
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
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	entry, found := rc.cache.Get(key)
	if !found {
		return nil, false
	}

	// Check TTL
	if time.Since(entry.timestamp) > rc.config.TTL {
		rc.mu.RUnlock()
		rc.mu.Lock()
		rc.cache.Evict(key)
		rc.mu.Unlock()
		rc.mu.RLock()
		return nil, false
	}

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
	body       *bytes.Buffer
	statusCode int
}

func (rec *responseRecorder) Write(buf []byte) (int, error) {
	rec.body.Write(buf)
	return rec.ResponseWriter.Write(buf)
}

func (rec *responseRecorder) WriteHeader(statusCode int) {
	rec.statusCode = statusCode
	rec.ResponseWriter.WriteHeader(statusCode)
}
