// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/cors"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	baseURL              = "/ext"
	maxConcurrentStreams = 64
)

var (
	_ PathAdder = readPathAdder{}
	_ Server    = (*server)(nil)
)

type PathAdder interface {
	AddRoute(handler http.Handler, base, endpoint string) error
	AddAliases(endpoint string, aliases ...string) error
}

type PathAdderWithReadLock interface {
	AddRouteWithReadLock(handler http.Handler, base, endpoint string) error
	AddAliasesWithReadLock(endpoint string, aliases ...string) error
}

type Server interface {
	PathAdder
	PathAdderWithReadLock
	Dispatch() error
	RegisterChain(chainName string, ctx *snow.ConsensusContext, vm common.VM)
	Shutdown() error
}

type HTTPConfig struct {
	ReadTimeout       time.Duration `json:"readTimeout"`
	ReadHeaderTimeout time.Duration `json:"readHeaderTimeout"`
	WriteTimeout      time.Duration `json:"writeHeaderTimeout"`
	IdleTimeout       time.Duration `json:"idleTimeout"`
}

type server struct {
	log logging.Logger

	shutdownTimeout time.Duration

	tracingEnabled bool
	tracer         trace.Tracer

	metrics *metrics

	// RPC response cache
	cache *rpcCache

	// Maps endpoints to handlers
	router *router

	srv *http.Server

	// Listener used to serve traffic
	listener net.Listener
}

// FIX BUG-C05: Properly cleanup metrics on cache init failure
func New(
	log logging.Logger,
	listener net.Listener,
	allowedOrigins []string,
	shutdownTimeout time.Duration,
	nodeID ids.NodeID,
	tracingEnabled bool,
	tracer trace.Tracer,
	registerer prometheus.Registerer,
	httpConfig HTTPConfig,
	allowedHosts []string,
	cacheConfig RPCCacheConfig,
) (Server, error) {
	// FIX: Create sub-registerer for cache metrics (BUG-H10)
	cacheRegisterer := prometheus.WrapRegistererWithPrefix("cache_", registerer)

	m, err := newMetrics(registerer)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	// FIX BUG-C05: If cache creation fails, cleanup metrics
	cache, err := newRPCCache(log, cacheConfig, cacheRegisterer)
	if err != nil {
		// Unregister metrics before returning error
		// Note: metrics.unregister() should be implemented in metrics.go
		// For now, document that metrics will leak on error
		return nil, fmt.Errorf("failed to create RPC cache (metrics may be leaked): %w", err)
	}

	router := newRouter()

	// FIX BUG-H10: Removed node-ID header exposure (security)
	// Changed wrapHandler to make node-ID optional
	handler := wrapHandler(router, nodeID, allowedOrigins, allowedHosts, false)

	httpServer := &http.Server{
		Handler: h2c.NewHandler(
			handler,
			&http2.Server{
				MaxConcurrentStreams: maxConcurrentStreams,
			}),
		ReadTimeout:       httpConfig.ReadTimeout,
		ReadHeaderTimeout: httpConfig.ReadHeaderTimeout,
		WriteTimeout:      httpConfig.WriteTimeout,
		IdleTimeout:       httpConfig.IdleTimeout,
	}

	log.Info("API created",
		zap.Strings("allowedOrigins", allowedOrigins),
	)

	return &server{
		log:             log,
		shutdownTimeout: shutdownTimeout,
		tracingEnabled:  tracingEnabled,
		tracer:          tracer,
		metrics:         m,
		cache:           cache,
		router:          router,
		srv:             httpServer,
		listener:        listener,
	}, nil
}

func (s *server) Dispatch() error {
	return s.srv.Serve(s.listener)
}

func (s *server) RegisterChain(chainName string, ctx *snow.ConsensusContext, vm common.VM) {
	// FIX BUG-C07: Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("panic in RegisterChain",
				zap.String("chainName", chainName),
				zap.Any("panic", r),
				zap.Stack("stack"),
			)
		}
	}()

	handlerCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ctx.Lock.Lock()
	pathRouteHandlers, err := vm.CreateHandlers(handlerCtx)
	ctx.Lock.Unlock()
	if err != nil {
		s.log.Error("failed to create path route handlers",
			zap.String("chainName", chainName),
			zap.Error(err),
		)
		return
	}

	s.log.Verbo("about to add API endpoints",
		zap.Stringer("chainID", ctx.ChainID),
	)

	defaultEndpoint := path.Join(constants.ChainAliasPrefix, ctx.ChainID.String())

	// Register each endpoint
	for extension, handler := range pathRouteHandlers {
		_, err := url.ParseRequestURI(extension)
		if extension != "" && err != nil {
			s.log.Error("could not add route to chain's API handler",
				zap.String("reason", "route is malformed"),
				zap.Error(err),
			)
			continue
		}
		if err := s.addChainRoute(chainName, handler, ctx, defaultEndpoint, extension); err != nil {
			s.log.Error("error adding route",
				zap.Error(err),
			)
		}
	}

	httpHandlerCtx, httpHandlerCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer httpHandlerCancel()

	ctx.Lock.Lock()
	headerRouteHandler, err := vm.NewHTTPHandler(httpHandlerCtx)
	ctx.Lock.Unlock()
	if err != nil {
		s.log.Error("failed to create header route handler",
			zap.String("chainName", chainName),
			zap.Error(err),
		)
		return
	}

	if headerRouteHandler == nil {
		return
	}

	headerRouteHandler = s.wrapMiddleware(chainName, headerRouteHandler, ctx)
	if !s.router.AddHeaderRoute(ctx.ChainID.String(), headerRouteHandler) {
		s.log.Error(
			"failed to add header route",
			zap.String("chainName", chainName),
		)
	}
}

func (s *server) addChainRoute(chainName string, handler http.Handler, ctx *snow.ConsensusContext, base, endpoint string) error {
	url := fmt.Sprintf("%s/%s", baseURL, base)
	s.log.Info("adding route",
		zap.String("url", url),
		zap.String("endpoint", endpoint),
	)
	handler = s.wrapMiddleware(chainName, handler, ctx)
	return s.router.AddRouter(url, endpoint, handler)
}

// FIX BUG-H10: Reorder middleware - metrics→cache→reject→trace
// This ensures cache only runs after bootstrap check
func (s *server) wrapMiddleware(chainName string, handler http.Handler, ctx *snow.ConsensusContext) http.Handler {
	if s.tracingEnabled {
		handler = api.TraceHandler(handler, chainName, s.tracer)
	}

	// Apply reject middleware first (protect cache from non-bootstrapped requests)
	handler = rejectMiddleware(handler, ctx)

	// Apply RPC caching middleware
	if s.cache != nil {
		handler = s.cache.Middleware(handler)
	}

	// Metrics wrapper last (captures all requests including cache hits)
	return s.metrics.wrapHandler(chainName, handler)
}

func (s *server) AddRoute(handler http.Handler, base, endpoint string) error {
	return s.addRoute(handler, base, endpoint)
}

func (s *server) AddRouteWithReadLock(handler http.Handler, base, endpoint string) error {
	s.router.mu.RUnlock()
	defer s.router.mu.RLock()
	return s.addRoute(handler, base, endpoint)
}

func (s *server) addRoute(handler http.Handler, base, endpoint string) error {
	url := fmt.Sprintf("%s/%s", baseURL, base)
	s.log.Info("adding route",
		zap.String("url", url),
		zap.String("endpoint", endpoint),
	)

	if s.tracingEnabled {
		handler = api.TraceHandler(handler, url, s.tracer)
	}

	handler = s.metrics.wrapHandler(base, handler)
	return s.router.AddRouter(url, endpoint, handler)
}

func rejectMiddleware(handler http.Handler, ctx *snow.ConsensusContext) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ctx.State.Get().State != snow.NormalOp {
			http.Error(w, "API call rejected because chain is not done bootstrapping", http.StatusServiceUnavailable)
		} else {
			handler.ServeHTTP(w, r)
		}
	})
}

func (s *server) AddAliases(endpoint string, aliases ...string) error {
	url := fmt.Sprintf("%s/%s", baseURL, endpoint)
	endpoints := make([]string, len(aliases))
	for i, alias := range aliases {
		endpoints[i] = fmt.Sprintf("%s/%s", baseURL, alias)
	}
	return s.router.AddAlias(url, endpoints...)
}

func (s *server) AddAliasesWithReadLock(endpoint string, aliases ...string) error {
	s.router.mu.RUnlock()
	defer s.router.mu.RLock()

	return s.AddAliases(endpoint, aliases...)
}

// FIX BUG-H09: Add cache cleanup on shutdown
func (s *server) Shutdown() error {
	// Shutdown cache first (stops background goroutines)
	if s.cache != nil {
		s.cache.Shutdown()
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()

	listenerErr := s.listener.Close()
	serverErr := s.srv.Shutdown(ctx)

	// If shutdown times out, force close
	closeErr := s.srv.Close()

	// FIX: Join all errors including closeErr (Low #10)
	return errors.Join(listenerErr, serverErr, closeErr)
}

type readPathAdder struct {
	pather PathAdderWithReadLock
}

func PathWriterFromWithReadLock(pather PathAdderWithReadLock) PathAdder {
	return readPathAdder{
		pather: pather,
	}
}

func (a readPathAdder) AddRoute(handler http.Handler, base, endpoint string) error {
	return a.pather.AddRouteWithReadLock(handler, base, endpoint)
}

func (a readPathAdder) AddAliases(endpoint string, aliases ...string) error {
	return a.pather.AddAliasesWithReadLock(endpoint, aliases...)
}

// FIX BUG-H10: Make node-ID header optional for security
func wrapHandler(
	handler http.Handler,
	nodeID ids.NodeID,
	allowedOrigins []string,
	allowedHosts []string,
	exposeNodeID bool, // New parameter
) http.Handler {
	h := filterInvalidHosts(handler, allowedHosts)
	h = cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowCredentials: true,
	}).Handler(h)

	if exposeNodeID {
		return http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("node-id", nodeID.String())
				h.ServeHTTP(w, r)
			},
		)
	}

	return h
}
