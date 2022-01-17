// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/NYTimes/gziphandler"

	"github.com/gorilla/handlers"

	"github.com/rs/cors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const baseURL = "/ext"

var (
	errUnknownLockOption = errors.New("invalid lock options")

	_ RouteAdder = &Server{}
)

type RouteAdder interface {
	AddRoute(handler *common.HTTPHandler, lock *sync.RWMutex, base, endpoint string, loggingWriter io.Writer) error
}

// Server maintains the HTTP router
type Server struct {
	// log this server writes to
	log logging.Logger
	// generates new logs for chains to write to
	factory logging.Factory
	// points the the router handlers
	handler http.Handler
	// Listens for HTTP traffic on this address
	listenHost string
	listenPort uint16

	shutdownTimeout time.Duration

	// Maps endpoints to handlers
	router *router

	srv *http.Server
}

// Initialize creates the API server at the provided host and port
func (s *Server) Initialize(
	log logging.Logger,
	factory logging.Factory,
	host string,
	port uint16,
	allowedOrigins []string,
	shutdownTimeout time.Duration,
	nodeID ids.ShortID,
	wrappers ...Wrapper,
) {
	s.log = log
	s.factory = factory
	s.listenHost = host
	s.listenPort = port
	s.shutdownTimeout = shutdownTimeout
	s.router = newRouter()

	s.log.Info("API created with allowed origins: %v", allowedOrigins)

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowCredentials: true,
	}).Handler(s.router)
	gzipHandler := gziphandler.GzipHandler(corsHandler)
	s.handler = http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Attach this node's ID as a header
			w.Header().Set("node-id", nodeID.PrefixedString(constants.NodeIDPrefix))
			gzipHandler.ServeHTTP(w, r)
		},
	)

	for _, wrapper := range wrappers {
		s.handler = wrapper.WrapHandler(s.handler)
	}
}

// Dispatch starts the API server
func (s *Server) Dispatch() error {
	listenAddress := fmt.Sprintf("%s:%d", s.listenHost, s.listenPort)
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return err
	}

	ipDesc, err := utils.ToIPDesc(listener.Addr().String())
	if err != nil {
		s.log.Info("HTTP API server listening on %q", listenAddress)
	} else {
		s.log.Info("HTTP API server listening on \"%s:%d\"", s.listenHost, ipDesc.Port)
	}

	s.srv = &http.Server{Handler: s.handler}
	return s.srv.Serve(listener)
}

// DispatchTLS starts the API server with the provided TLS certificate
func (s *Server) DispatchTLS(certBytes, keyBytes []byte) error {
	listenAddress := fmt.Sprintf("%s:%d", s.listenHost, s.listenPort)
	cert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return err
	}
	config := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}

	listener, err := tls.Listen("tcp", listenAddress, config)
	if err != nil {
		return err
	}

	ipDesc, err := utils.ToIPDesc(listener.Addr().String())
	if err != nil {
		s.log.Info("HTTPS API server listening on %q", listenAddress)
	} else {
		s.log.Info("HTTPS API server listening on \"%s:%d\"", s.listenHost, ipDesc.Port)
	}

	s.srv = &http.Server{Addr: listenAddress, Handler: s.handler}
	return s.srv.Serve(listener)
}

// RegisterChain registers the API endpoints associated with this chain. That is,
// add <route, handler> pairs to server so that API calls can be made to the VM.
// This method runs in a goroutine to avoid a deadlock in the event that the caller
// holds the engine's context lock. Namely, this could happen when the P-Chain is
// creating a new chain and holds the P-Chain's lock when this function is held,
// and at the same time the server's lock is held due to an API call and is trying
// to grab the P-Chain's lock.
func (s *Server) RegisterChain(chainName string, engine common.Engine) {
	go s.registerChain(chainName, engine)
}

func (s *Server) registerChain(chainName string, engine common.Engine) {
	var (
		handlers map[string]*common.HTTPHandler
		err      error
	)

	ctx := engine.Context()
	ctx.Lock.Lock()
	handlers, err = engine.GetVM().CreateHandlers()
	ctx.Lock.Unlock()
	if err != nil {
		s.log.Error("failed to create %s handlers: %s", chainName, err)
		return
	}

	httpLogger, err := s.factory.MakeChainChild(chainName, "http")
	if err != nil {
		s.log.Error("failed to create new http logger: %s", err)
		return
	}

	s.log.Verbo("About to add API endpoints for chain with ID %s", ctx.ChainID)
	// all subroutes to a chain begin with "bc/<the chain's ID>"
	defaultEndpoint := constants.ChainAliasPrefix + ctx.ChainID.String()

	// Register each endpoint
	for extension, handler := range handlers {
		// Validate that the route being added is valid
		// e.g. "/foo" and "" are ok but "\n" is not
		_, err := url.ParseRequestURI(extension)
		if extension != "" && err != nil {
			s.log.Error("could not add route to chain's API handler because route is malformed: %s", err)
			continue
		}
		if err := s.AddChainRoute(handler, ctx, defaultEndpoint, extension, httpLogger); err != nil {
			s.log.Error("error adding route: %s", err)
		}
	}
}

// AddChainRoute registers a route to a chain's handler
func (s *Server) AddChainRoute(handler *common.HTTPHandler, ctx *snow.ConsensusContext, base, endpoint string, loggingWriter io.Writer) error {
	url := fmt.Sprintf("%s/%s", baseURL, base)
	s.log.Info("adding route %s%s", url, endpoint)
	// Apply logging middleware
	h := handlers.CombinedLoggingHandler(loggingWriter, handler.Handler)
	// Apply middleware to grab/release chain's lock before/after calling API method
	h, err := lockMiddleware(h, handler.LockOptions, &ctx.Lock)
	if err != nil {
		return err
	}
	// Apply middleware to reject calls to the handler before the chain finishes bootstrapping
	h = rejectMiddleware(h, ctx)
	return s.router.AddRouter(url, endpoint, h)
}

// AddRoute registers a route to a handler.
func (s *Server) AddRoute(handler *common.HTTPHandler, lock *sync.RWMutex, base, endpoint string, loggingWriter io.Writer) error {
	url := fmt.Sprintf("%s/%s", baseURL, base)
	s.log.Info("adding route %s%s", url, endpoint)
	// Apply logging middleware
	h := handlers.CombinedLoggingHandler(loggingWriter, handler.Handler)
	// Apply middleware to grab/release chain's lock before/after calling API method
	h, err := lockMiddleware(h, handler.LockOptions, lock)
	if err != nil {
		return err
	}
	return s.router.AddRouter(url, endpoint, h)
}

// Wraps a handler by grabbing and releasing a lock before calling the handler.
func lockMiddleware(handler http.Handler, lockOption common.LockOption, lock *sync.RWMutex) (http.Handler, error) {
	switch lockOption {
	case common.WriteLock:
		return middlewareHandler{
			before:  lock.Lock,
			after:   lock.Unlock,
			handler: handler,
		}, nil
	case common.ReadLock:
		return middlewareHandler{
			before:  lock.RLock,
			after:   lock.RUnlock,
			handler: handler,
		}, nil
	case common.NoLock:
		return handler, nil
	default:
		return nil, errUnknownLockOption
	}
}

// Reject middleware wraps a handler. If the chain that the context describes is
// not done bootstrapping, writes back an error.
func rejectMiddleware(handler http.Handler, ctx *snow.ConsensusContext) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { // If chain isn't done bootstrapping, ignore API calls
		if ctx.GetState() != snow.NormalOp {
			w.WriteHeader(http.StatusServiceUnavailable)
			// Doesn't matter if there's an error while writing. They'll get the StatusServiceUnavailable code.
			_, _ = w.Write([]byte("API call rejected because chain is not done bootstrapping"))
		} else {
			handler.ServeHTTP(w, r)
		}
	})
}

// AddAliases registers aliases to the server
func (s *Server) AddAliases(endpoint string, aliases ...string) error {
	url := fmt.Sprintf("%s/%s", baseURL, endpoint)
	endpoints := make([]string, len(aliases))
	for i, alias := range aliases {
		endpoints[i] = fmt.Sprintf("%s/%s", baseURL, alias)
	}
	return s.router.AddAlias(url, endpoints...)
}

// AddAliasesWithReadLock registers aliases to the server assuming the http read
// lock is currently held.
func (s *Server) AddAliasesWithReadLock(endpoint string, aliases ...string) error {
	// This is safe, as the read lock doesn't actually need to be held once the
	// http handler is called. However, it is unlocked later, so this function
	// must end with the lock held.
	s.router.lock.RUnlock()
	defer s.router.lock.RLock()

	return s.AddAliases(endpoint, aliases...)
}

// Shutdown this server
func (s *Server) Shutdown() error {
	if s.srv == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	err := s.srv.Shutdown(ctx)
	cancel()

	// If shutdown times out, make sure the server is still shutdown.
	_ = s.srv.Close()
	return err
}
