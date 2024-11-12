// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/cors"
	"go.uber.org/zap"
	"golang.org/x/net/http2"

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
	// AddRoute registers a route to a handler.
	AddRoute(handler http.Handler, base, endpoint string) error

	// AddAliases registers aliases to the server
	AddAliases(endpoint string, aliases ...string) error
}

type PathAdderWithReadLock interface {
	// AddRouteWithReadLock registers a route to a handler assuming the http
	// read lock is currently held.
	AddRouteWithReadLock(handler http.Handler, base, endpoint string) error

	// AddAliasesWithReadLock registers aliases to the server assuming the http read
	// lock is currently held.
	AddAliasesWithReadLock(endpoint string, aliases ...string) error
}

// Server maintains the HTTP router
type Server interface {
	PathAdder
	PathAdderWithReadLock
	// Dispatch starts the API server
	Dispatch() error
	// RegisterChain registers the API endpoints associated with this chain.
	// That is, add <route, handler> pairs to server so that API calls can be
	// made to the VM.
	RegisterChain(chainName string, ctx *snow.ConsensusContext, vm common.VM)
	// Shutdown this server
	Shutdown() error
}

type HTTPConfig struct {
	ReadTimeout       time.Duration `json:"readTimeout"`
	ReadHeaderTimeout time.Duration `json:"readHeaderTimeout"`
	WriteTimeout      time.Duration `json:"writeHeaderTimeout"`
	IdleTimeout       time.Duration `json:"idleTimeout"`
}

type server struct {
	// log this server writes to
	log logging.Logger
	// generates new logs for chains to write to
	factory logging.Factory

	shutdownTimeout time.Duration

	tracingEnabled bool
	tracer         trace.Tracer

	metrics *metrics

	// Maps endpoints to handlers
	router *router

	srv *http.Server

	// Listener used to serve traffic
	listener net.Listener
}

// New returns an instance of a Server.
func New(
	log logging.Logger,
	factory logging.Factory,
	listener net.Listener,
	allowedOrigins []string,
	shutdownTimeout time.Duration,
	nodeID ids.NodeID,
	tracingEnabled bool,
	tracer trace.Tracer,
	registerer prometheus.Registerer,
	httpConfig HTTPConfig,
	allowedHosts []string,
) (Server, error) {
	m, err := newMetrics(registerer)
	if err != nil {
		return nil, err
	}

	router := newRouter()
	allowedHostsHandler := filterInvalidHosts(router, allowedHosts)
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowCredentials: true,
	}).Handler(allowedHostsHandler)
	gzipHandler := gziphandler.GzipHandler(corsHandler)
	var handler http.Handler = http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Attach this node's ID as a header
			w.Header().Set("node-id", nodeID.String())
			gzipHandler.ServeHTTP(w, r)
		},
	)

	httpServer := &http.Server{
		Handler:           handler,
		ReadTimeout:       httpConfig.ReadTimeout,
		ReadHeaderTimeout: httpConfig.ReadHeaderTimeout,
		WriteTimeout:      httpConfig.WriteTimeout,
		IdleTimeout:       httpConfig.IdleTimeout,
	}
	err = http2.ConfigureServer(httpServer, &http2.Server{
		MaxConcurrentStreams: maxConcurrentStreams,
	})
	if err != nil {
		return nil, err
	}

	log.Info("API created",
		zap.Strings("allowedOrigins", allowedOrigins),
	)

	return &server{
		log:             log,
		factory:         factory,
		shutdownTimeout: shutdownTimeout,
		tracingEnabled:  tracingEnabled,
		tracer:          tracer,
		metrics:         m,
		router:          router,
		srv:             httpServer,
		listener:        listener,
	}, nil
}

func (s *server) Dispatch() error {
	return s.srv.Serve(s.listener)
}

func (s *server) RegisterChain(chainName string, ctx *snow.ConsensusContext, vm common.VM) {
	ctx.Lock.Lock()
	handlers, err := vm.CreateHandlers(context.TODO())
	ctx.Lock.Unlock()
	if err != nil {
		s.log.Error("failed to create handlers",
			zap.String("chainName", chainName),
			zap.Error(err),
		)
		return
	}

	s.log.Verbo("about to add API endpoints",
		zap.Stringer("chainID", ctx.ChainID),
	)
	// all subroutes to a chain begin with "bc/<the chain's ID>"
	defaultEndpoint := path.Join(constants.ChainAliasPrefix, ctx.ChainID.String())

	// Register each endpoint
	for extension, handler := range handlers {
		// Validate that the route being added is valid
		// e.g. "/foo" and "" are ok but "\n" is not
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
}

func (s *server) addChainRoute(chainName string, handler http.Handler, ctx *snow.ConsensusContext, base, endpoint string) error {
	url := fmt.Sprintf("%s/%s", baseURL, base)
	s.log.Info("adding route",
		zap.String("url", url),
		zap.String("endpoint", endpoint),
	)
	if s.tracingEnabled {
		handler = api.TraceHandler(handler, chainName, s.tracer)
	}
	// Apply middleware to reject calls to the handler before the chain finishes bootstrapping
	handler = rejectMiddleware(handler, ctx)
	handler = s.metrics.wrapHandler(chainName, handler)
	return s.router.AddRouter(url, endpoint, handler)
}

func (s *server) AddRoute(handler http.Handler, base, endpoint string) error {
	return s.addRoute(handler, base, endpoint)
}

func (s *server) AddRouteWithReadLock(handler http.Handler, base, endpoint string) error {
	s.router.lock.RUnlock()
	defer s.router.lock.RLock()
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

// Reject middleware wraps a handler. If the chain that the context describes is
// not done state-syncing/bootstrapping, writes back an error.
func rejectMiddleware(handler http.Handler, ctx *snow.ConsensusContext) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { // If chain isn't done bootstrapping, ignore API calls
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
	// This is safe, as the read lock doesn't actually need to be held once the
	// http handler is called. However, it is unlocked later, so this function
	// must end with the lock held.
	s.router.lock.RUnlock()
	defer s.router.lock.RLock()

	return s.AddAliases(endpoint, aliases...)
}

func (s *server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	err := s.srv.Shutdown(ctx)
	cancel()

	// If shutdown times out, make sure the server is still shutdown.
	_ = s.srv.Close()
	return err
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
