// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"net/http"
	"sync"
)

// httpHandler that wraps an existing [http.Handler]. If the existing
// [http.Handler] is nil, this handler returns with [http.StatusNotFound].
// Otherwise, it delegates calls to the inner handler.
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

// httpHandlers manages an append-only collection of updateable routes.
type httpHandlers struct {
	lock   sync.RWMutex
	routes map[string]*httpHandler
}

// set adds all the new handlers to the collection of tracked routes. Any
// tracked routes not included in newHandlers will still be tracked, but will
// return 404s.
func (h *httpHandlers) set(newHandlers map[string]http.Handler) {
	h.lock.Lock()
	defer h.lock.Unlock()

	for path, newHandler := range newHandlers {
		handler := &httpHandler{}
		if oldHandler, ok := h.routes[path]; ok {
			handler = oldHandler
		}
		handler.set(newHandler)
	}
	for path, oldHandler := range h.routes {
		if _, ok := newHandlers[path]; ok {
			continue
		}
		oldHandler.set(nil)
	}
}

// asInterface returns the current collection of tracked handlers as their
// interface type.
func (h *httpHandlers) asInterface() map[string]http.Handler {
	h.lock.RLock()
	defer h.lock.RUnlock()

	handlers := make(map[string]http.Handler, len(h.routes))
	for path, handler := range h.routes {
		handlers[path] = handler
	}
	return handlers
}

func (v *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	newHandlers, err := v.current.chain.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	v.current.httpHandlers.set(newHandlers)
	return v.current.httpHandlers.asInterface(), nil
}

// None of Subnet-EVM, Coreth, or SAEVM implement NewHTTPHandler, so it is left
// unimplemented.
func (v *VM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	return nil, nil
}
