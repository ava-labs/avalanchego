// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"net/http"
	"sync"
)

// httpHandler wraps an [http.Handler], serving 404 when it is nil.
type httpHandler struct {
	lock    sync.RWMutex
	handler http.Handler
}

func (h *httpHandler) set(handler http.Handler) {
	h.lock.Lock()
	defer h.lock.Unlock()

	h.handler = handler
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lock.RLock()
	handler := h.handler
	h.lock.RUnlock()

	if handler == nil {
		http.NotFound(w, r)
		return
	}

	handler.ServeHTTP(w, r)
}

// httpHandlers is an append-only collection of updatable routes.
type httpHandlers struct {
	lock   sync.RWMutex
	routes map[string]*httpHandler
}

func newHTTPHandlers() *httpHandlers {
	return &httpHandlers{
		routes: make(map[string]*httpHandler),
	}
}

// set rebinds tracked routes to newHandlers. Routes absent from newHandlers
// are kept but serve 404.
func (h *httpHandlers) set(newHandlers map[string]http.Handler) {
	h.lock.Lock()
	defer h.lock.Unlock()

	for path, newHandler := range newHandlers {
		handler := &httpHandler{}
		if oldHandler, ok := h.routes[path]; ok {
			handler = oldHandler
		}
		handler.set(newHandler)
		h.routes[path] = handler
	}
	for path, oldHandler := range h.routes {
		if _, ok := newHandlers[path]; ok {
			continue
		}
		oldHandler.set(nil)
	}
}

// asInterface returns the tracked routes as an [http.Handler] map.
func (h *httpHandlers) asInterface() map[string]http.Handler {
	h.lock.RLock()
	defer h.lock.RUnlock()

	handlers := make(map[string]http.Handler, len(h.routes))
	for path, handler := range h.routes {
		handlers[path] = handler
	}
	return handlers
}

func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	newHandlers, err := vm.current.chain.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	vm.httpHandlers.set(newHandlers)
	return vm.httpHandlers.asInterface(), nil
}

// None of Subnet-EVM, Coreth, or SAEVM implement NewHTTPHandler, so it is left
// unimplemented.
func (*VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	return nil, nil
}
