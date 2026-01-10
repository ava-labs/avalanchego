// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

const HTTPHeaderRoute = "Avalanche-Api-Route"

var (
	errUnknownBaseURL  = errors.New("unknown base url")
	errUnknownEndpoint = errors.New("unknown endpoint")
	errAlreadyReserved = errors.New("route is either already aliased or already maps to a handle")
)

// FIX BUG-C04, BUG-H06: Use single mutex for all operations
// Document lock protection clearly
type router struct {
	mu sync.RWMutex // Protects ALL fields below

	router *mux.Router

	// Routes and aliases
	reservedRoutes set.Set[string]     // Reserves routes so aliases can't conflict
	aliases        map[string][]string // Maps a route to reserved routes
	headerRoutes   map[string]http.Handler
	routes         map[string]map[string]http.Handler // Maps routes to handlers
}

func newRouter() *router {
	return &router{
		router:         mux.NewRouter(),
		reservedRoutes: set.Set[string]{},
		aliases:        make(map[string][]string),
		headerRoutes:   make(map[string]http.Handler),
		routes:         make(map[string]map[string]http.Handler),
	}
}

func (r *router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	route, ok := request.Header[HTTPHeaderRoute]
	if !ok {
		// Fall-back to legacy path-based routing
		r.router.ServeHTTP(writer, request)
		return
	}

	// Request specified the routing header key but no value
	if len(route) < 1 {
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

// FIX BUG-H06: Consistent lock usage - use mu.RLock for reads
func (r *router) GetHandler(base, endpoint string) (http.Handler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	urlBase, exists := r.routes[base]
	if !exists {
		return nil, errUnknownBaseURL
	}
	handler, exists := urlBase[endpoint]
	if !exists {
		return nil, errUnknownEndpoint
	}
	return handler, nil
}

func (r *router) AddHeaderRoute(route string, handler http.Handler) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok := r.headerRoutes[route]
	if ok {
		return false
	}

	r.headerRoutes[route] = handler
	return true
}

// FIX BUG-C04: Remove double locking - use single mutex
func (r *router) AddRouter(base, endpoint string, handler http.Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.addRouter(base, endpoint, handler)
}

func (r *router) addRouter(base, endpoint string, handler http.Handler) error {
	if r.reservedRoutes.Contains(base) {
		return fmt.Errorf("%w: %s", errAlreadyReserved, base)
	}

	return r.forceAddRouter(base, endpoint, handler)
}

// FIX BUG-C11: Accumulate all errors using errors.Join
func (r *router) forceAddRouter(base, endpoint string, handler http.Handler) error {
	endpoints := r.routes[base]
	if endpoints == nil {
		endpoints = make(map[string]http.Handler)
	}
	url := base + endpoint
	if _, exists := endpoints[endpoint]; exists {
		// FIX: Don't leak internal paths in error (Low #8)
		return fmt.Errorf("endpoint already exists for this route")
	}

	endpoints[endpoint] = handler
	r.routes[base] = endpoints

	// Name routes based on their URL for easy retrieval
	route := r.router.Handle(url, handler)
	if route == nil {
		return fmt.Errorf("failed to create new route")
	}
	route.Name(url)

	// FIX BUG-C11: Collect all errors when adding aliases
	var errs []error
	if aliases, exists := r.aliases[base]; exists {
		for _, alias := range aliases {
			if err := r.forceAddRouter(alias, endpoint, handler); err != nil {
				errs = append(errs, fmt.Errorf("failed to add alias %s: %w", alias, err))
			}
		}
	}

	// Return joined errors if any
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// FIX BUG-C04, BUG-C11: Remove double locking and fix error accumulation
func (r *router) AddAlias(base string, aliases ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check all aliases are available first
	for _, alias := range aliases {
		if r.reservedRoutes.Contains(alias) {
			return fmt.Errorf("%w: %s", errAlreadyReserved, alias)
		}
	}

	// Reserve all aliases
	for _, alias := range aliases {
		r.reservedRoutes.Add(alias)
	}

	r.aliases[base] = append(r.aliases[base], aliases...)

	// FIX BUG-C11: Collect all errors
	var errs []error
	if endpoints, exists := r.routes[base]; exists {
		for endpoint, handler := range endpoints {
			for _, alias := range aliases {
				if err := r.forceAddRouter(alias, endpoint, handler); err != nil {
					errs = append(errs, fmt.Errorf("failed to add route for alias %s endpoint %s: %w", alias, endpoint, err))
				}
			}
		}
	}

	// Return joined errors if any
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
