// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/mux"

	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	// TODO normalize lowercase?
	HTTPHeaderRoute = "Avalanche-API-Route"

	errUnknownEndpoint = errors.New("unknown endpoint")
	errAlreadyReserved = errors.New("route is either already aliased or already maps to a handle")
)

type router struct {
	lock   sync.RWMutex
	router *mux.Router

	routeLock      sync.Mutex
	reservedRoutes set.Set[string]     // Reserves routes so that there can't be alias that conflict
	aliases        map[string][]string // Maps a route to a set of reserved routes
	// headerRoutes contains routes based on http headers
	// aliasing is not currently supported
	headerRoutes map[string]http.Handler
	// legacy url-based routing
	routes map[string]http.Handler // Maps routes to a handler
}

func newRouter() *router {
	return &router{
		router:         mux.NewRouter(),
		reservedRoutes: set.Set[string]{},
		aliases:        make(map[string][]string),
		headerRoutes:   make(map[string]http.Handler),
		routes:         make(map[string]http.Handler),
	}
}

func (r *router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	route, ok := request.Header[http.CanonicalHeaderKey(HTTPHeaderRoute)]
	if !ok {
		// If there is no routing header, fall-back to the legacy path-based
		// routing
		r.router.ServeHTTP(writer, request)
		return
	}

	// Request specified the routing header key but did not provide a
	// corresponding value
	if len(route) == 0 {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	handler, ok := r.headerRoutes[route[0]]
	if !ok {
		writer.WriteHeader(http.StatusNotFound)
		return
	}

	handler.ServeHTTP(writer, request)
}

func (r *router) GetHandler(endpoint string) (http.Handler, error) {
	r.routeLock.Lock()
	defer r.routeLock.Unlock()

	handler, exists := r.routes[endpoint]
	if !exists {
		return nil, errUnknownEndpoint
	}

	return handler, nil
}

func (r *router) AddHeaderRoute(route string, handler http.Handler) bool {
	// TODO which lock do i use
	r.lock.Lock()
	defer r.lock.Unlock()

	_, ok := r.headerRoutes[route]
	if ok {
		return false
	}

	r.headerRoutes[route] = handler
	return true
}

func (r *router) AddRouter(endpoint string, handler http.Handler) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.routeLock.Lock()
	defer r.routeLock.Unlock()

	return r.addRouter(endpoint, handler)
}

func (r *router) addRouter(endpoint string, handler http.Handler) error {
	if r.reservedRoutes.Contains(endpoint) {
		return fmt.Errorf("%w: %s", errAlreadyReserved, endpoint)
	}

	return r.forceAddRouter(endpoint, handler)
}

func (r *router) forceAddRouter(endpoint string, handler http.Handler) error {
	_, ok := r.routes[endpoint]
	if ok {
		return fmt.Errorf("failed to create endpoint as %s already exists", endpoint)
	}

	r.routes[endpoint] = handler

	// Name routes based on their URL for easy retrieval in the future
	route := r.router.Handle(endpoint, handler)
	if route == nil {
		return fmt.Errorf("failed to create new route for %s", endpoint)
	}
	route.Name(endpoint)

	var err error
	if aliases, exists := r.aliases[endpoint]; exists {
		for _, alias := range aliases {
			if innerErr := r.forceAddRouter(alias, handler); err == nil {
				err = innerErr
			}
		}
	}
	return err
}

func (r *router) AddAlias(base string, aliases ...string) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.routeLock.Lock()
	defer r.routeLock.Unlock()

	for _, alias := range aliases {
		if r.reservedRoutes.Contains(alias) {
			return fmt.Errorf("%w: %s", errAlreadyReserved, alias)
		}
	}

	for _, alias := range aliases {
		r.reservedRoutes.Add(alias)
	}

	r.aliases[base] = append(r.aliases[base], aliases...)

	var err error
	if handler, exists := r.routes[base]; exists {
		for _, alias := range aliases {
			if innerErr := r.forceAddRouter(alias, handler); err == nil {
				err = innerErr
			}
		}
	}
	return err
}
