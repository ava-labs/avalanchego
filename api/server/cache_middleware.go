// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	// DefaultCacheSize is the default number of cached RPC responses
	DefaultCacheSize = 10000

	// DefaultCacheTTL is the default time-to-live for cache entries
	DefaultCacheTTL = 5 * time.Minute

	// MaxResponseSize is the maximum size of a response to cache (10MB)
	MaxResponseSize = 10 * 1024 * 1024

	// MaxParamSizeForCanonicalization is the max param size for JSON canonicalization (100KB)
	// Larger params skip canonicalization to avoid excessive allocations
	MaxParamSizeForCanonicalization = 100 * 1024
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
	cacheableMethods set.Set[string]
}

// newRPCCache creates a new RPC cache instance
func newRPCCache(log logging.Logger, config RPCCacheConfig, registerer prometheus.Registerer) (*rpcCache, error) {
	if !config.Enabled {
		return nil, nil
	}

	// Validate config
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

	if err := errors.Join(
		registerer.Register(metrics.hits),
		registerer.Register(metrics.misses),
		registerer.Register(metrics.evictions),
		registerer.Register(metrics.size),
	); err != nil {
		return nil, err
	}

	cache := lru.NewCacheWithOnEvict(config.Size, func(key string, value *cacheEntry) {
		metrics.evictions.Inc()
		// size is managed by Set() after each mutation; no Dec() needed here
	})

	rc := &rpcCache{
		log:     log,
		config:  config,
		cache:   cache,
		metrics: metrics,
		cacheableMethods: set.Of(
			"eth_call",
			"eth_getBalance",
			"eth_getCode",
			"eth_getStorageAt",
			"eth_getBlockByNumber",
			"eth_getBlockByHash",
			"eth_getTransactionByHash",
			"eth_getTransactionReceipt",
			"eth_getBlockTransactionCountByNumber",
			"eth_getBlockTransactionCountByHash",
			"eth_getUncleCountByBlockNumber",
			"eth_getUncleCountByBlockHash",
			"eth_getTransactionCount",
			"eth_getLogs",
			"net_version",
			"web3_clientVersion",
			"web3_sha3",
		),
	}

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

		// Limit request body size to prevent DoS via large request bodies
		limitedReader := io.LimitReader(r.Body, MaxResponseSize+1)
		body, err := io.ReadAll(limitedReader)
		r.Body = io.NopCloser(bytes.NewBuffer(body))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if len(body) > MaxResponseSize {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}

		// JSON allows leading whitespace; trim before checking for batch requests
		trimmed := bytes.TrimLeft(body, " \t\n\r")
		if len(trimmed) > 0 && trimmed[0] == '[' {
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

		// Normalize nil/empty params for consistent cache keys
		if len(rpcReq.Params) == 0 {
			rpcReq.Params = json.RawMessage("[]")
		} else {
			paramStr := string(rpcReq.Params)
			if paramStr == "null" || paramStr == "{}" {
				rpcReq.Params = json.RawMessage("[]")
			} else if len(rpcReq.Params) <= MaxParamSizeForCanonicalization {
				// Canonicalize JSON for consistent cache keys regardless of whitespace;
				// skip large params to avoid excessive allocations
				var params interface{}
				if err := json.Unmarshal(rpcReq.Params, &params); err == nil {
					if canonical, err := json.Marshal(params); err == nil {
						rpcReq.Params = canonical
					} else {
						rc.log.Debug("failed to marshal canonical JSON, using original",
							zap.String("method", rpcReq.Method),
							zap.Error(err))
					}
				}
			}
		}

		// Generate cache key
		cacheKey := rc.generateKey(rpcReq.Method, rpcReq.Params)

		// Try to get from cache
		if entry, found := rc.get(cacheKey); found {
			rc.metrics.hits.Inc()
			// Copy response bytes — entry may be evicted and GC'd after lock release
			responseCopy := make([]byte, len(entry.response))
			copy(responseCopy, entry.response)

			// Deep copy headers to avoid sharing the cached map
			restoredHeaders := entry.headers.Clone()
			for k, v := range restoredHeaders {
				w.Header()[k] = v
			}
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(entry.status)
			if _, err := w.Write(responseCopy); err != nil {
				rc.log.Error("failed to write cached response", zap.Error(err))
			}
			return
		}

		rc.metrics.misses.Inc()
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
			if len(responseBytes) <= MaxResponseSize {
				headers := recorder.Header().Clone()

				responseCopy := make([]byte, len(responseBytes))
				copy(responseCopy, responseBytes)

				rc.put(cacheKey, &cacheEntry{
					response:  responseCopy,
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
	// Readonly flag enables caching of whitelisted read-only methods
	if !rc.config.Readonly {
		return false
	}
	return rc.cacheableMethods.Contains(method)
}

func (rc *rpcCache) generateKey(method string, params json.RawMessage) string {
	h := fnv.New64a()
	h.Write([]byte(method))
	h.Write([]byte{0}) // null separator prevents collisions from adjacent method/params bytes
	h.Write(params)
	return strconv.FormatUint(h.Sum64(), 16)
}

func (rc *rpcCache) get(key string) (*cacheEntry, bool) {
	// No defer — lock is manually managed to allow lock upgrade for TTL eviction
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
		// Re-check after acquiring write lock to handle concurrent TTL expiry
		entry, found = rc.cache.Get(key)
		if found && time.Since(entry.timestamp) > rc.config.TTL {
			rc.cache.Evict(key)
			rc.metrics.size.Set(float64(rc.cache.Len()))
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

// Flush removes all entries from the cache
func (rc *rpcCache) Flush() {
	if rc == nil {
		return
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.cache.Flush()
	rc.metrics.size.Set(0)
}

// responseRecorder captures the response for caching
type responseRecorder struct {
	http.ResponseWriter
	body          *bytes.Buffer
	statusCode    int
	headerWritten bool
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
	// Only record the first WriteHeader call
	if rec.headerWritten {
		return
	}
	rec.headerWritten = true
	rec.statusCode = statusCode
	rec.ResponseWriter.WriteHeader(statusCode)
}
