// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

var _ http.Handler = (*grpcRouter)(nil)

func newGRPCRouter() *grpcRouter {
	return &grpcRouter{
		handlers: make(map[string]http.Handler),
	}
}

type grpcRouter struct {
	lock     sync.RWMutex
	handlers map[string]http.Handler
}

func (g *grpcRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	// Requests take the form of /Prefix/Service/Method
	parsed := strings.Split(r.URL.Path, "/")
	if len(parsed) != 4 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get the unique chain id and service name pair
	prefixedService := prefixChainGRPCService(parsed[1], parsed[2])
	handler, ok := g.handlers[prefixedService]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Deep copy the request to avoid weird behavior from modifying r
	requestDeepCopy := r.Clone(r.Context())
	// Route this request to the grpc service using the chain prefix
	requestDeepCopy.URL.Path = strings.TrimPrefix(
		requestDeepCopy.URL.Path,
		"/"+parsed[1],
	)

	handler.ServeHTTP(w, requestDeepCopy)
}

func (g *grpcRouter) Add(chainID ids.ID, service string, handler http.Handler) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	prefixedService := prefixChainGRPCService(chainID.String(), service)
	if _, ok := g.handlers[prefixedService]; ok {
		return false
	}

	g.handlers[prefixedService] = handler
	return true
}

func prefixChainGRPCService(chainID string, service string) string {
	return path.Join(chainID, service)
}
