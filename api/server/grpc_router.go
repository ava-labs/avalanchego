// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
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
	handler, ok := g.handlers[path.Join(parsed[1], parsed[2])]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Route this request to the grpc service using the chain prefix
	requestCopy := *r
	requestCopy.URL = &(*r.URL)
	requestCopy.URL.Path = strings.TrimPrefix(
		requestCopy.URL.Path,
		fmt.Sprintf("/%s", parsed[1]),
	)

	handler.ServeHTTP(w, &requestCopy)
}

func (g *grpcRouter) Add(chainID string, service string, handler http.Handler) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	prefixedService := path.Join(chainID, service)
	if _, ok := g.handlers[prefixedService]; ok {
		return false
	}

	g.handlers[prefixedService] = handler
	return true
}
