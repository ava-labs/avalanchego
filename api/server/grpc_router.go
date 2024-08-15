// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

var (
	_ http.Handler = (*grpcRouter)(nil)

	ErrDuplicateHandler = errors.New("duplicate handler")
)

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

	// Requests take the form of "/Service/Method"
	parsed := strings.Split(r.RequestURI, "/")
	if len(parsed) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	handler, ok := g.handlers[parsed[1]]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	handler.ServeHTTP(w, r)
}

func (g *grpcRouter) Add(serviceName string, handler http.Handler) error {
	g.lock.Lock()
	defer g.lock.Unlock()

	if _, ok := g.handlers[serviceName]; ok {
		return fmt.Errorf("failed to register %s: %w", serviceName, ErrDuplicateHandler)
	}

	g.handlers[serviceName] = handler
	return nil
}
