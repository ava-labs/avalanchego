// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"context"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/lock"
)

// httpHandler wraps an [http.Handler], serving 404 when it is nil. New requests
// pass through its parent's gate and are counted as in-flight so they can be
// blocked and drained around a transition.
type httpHandler struct {
	parent *MutableHTTPHandlers

	handler utils.Atomic[http.Handler]
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Park until requests are unblocked. enter returns false only if the
	// request's context is canceled while parked, which here means the
	// connection closed; the client is gone, so there is no response to write.
	if !h.parent.enter(r.Context()) {
		return
	}
	defer h.parent.leave()

	handler := h.handler.Get()

	if handler == nil {
		http.NotFound(w, r)
		return
	}

	handler.ServeHTTP(w, r)
}

func (h *httpHandler) set(handler http.Handler) {
	h.handler.Set(handler)
}

// MutableHTTPHandlers is a collection of updatable routes. It can block new requests
// to every route and drain the in-flight ones, so an API implementation can
// safely change routing.
type MutableHTTPHandlers struct {
	lock sync.Mutex
	// cond is broadcast when blocked becomes false or inflight goes to 0,
	// waking any waiters in enter and Drain to re-check their conditions.
	cond     *lock.Cond
	routes   map[string]*httpHandler
	blocked  bool // whether new requests are parked
	inflight int  // number of requests currently being served
}

// NewMutableHTTPHandlers returns a new [MutableHTTPHandlers] with the given paths registered.
func NewMutableHTTPHandlers(paths ...string) *MutableHTTPHandlers {
	h := &MutableHTTPHandlers{
		routes: make(map[string]*httpHandler),
	}
	h.cond = lock.NewCond(&h.lock)
	for _, path := range paths {
		h.routes[path] = &httpHandler{parent: h}
	}
	return h
}

// Block parks new requests to every route until [MutableHTTPHandlers.Unblock] is
// called. In-flight requests are unaffected.
func (h *MutableHTTPHandlers) Block() {
	h.lock.Lock()
	defer h.lock.Unlock()

	h.blocked = true
}

// Unblock lets new requests through again, releasing any parked by
// [MutableHTTPHandlers.Block].
func (h *MutableHTTPHandlers) Unblock() {
	h.lock.Lock()
	defer h.lock.Unlock()

	h.blocked = false
	h.cond.Broadcast()
}

// Drain blocks until no requests are in flight or ctx is canceled, returning
// ctx's error in the latter case.
func (h *MutableHTTPHandlers) Drain(ctx context.Context) error {
	h.lock.Lock()
	defer h.lock.Unlock()

	for h.inflight > 0 {
		if err := h.cond.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

// enter parks while requests are blocked, then registers an in-flight request.
// If it returns true, [MutableHTTPHandlers.leave] MUST be called.
func (h *MutableHTTPHandlers) enter(ctx context.Context) bool {
	h.lock.Lock()
	defer h.lock.Unlock()

	for h.blocked {
		if err := h.cond.Wait(ctx); err != nil {
			return false
		}
	}
	h.inflight++
	return true
}

// leave records that an in-flight request returned, waking [MutableHTTPHandlers.Drain]
// if none remain.
func (h *MutableHTTPHandlers) leave() {
	h.lock.Lock()
	defer h.lock.Unlock()

	h.inflight--
	if h.inflight == 0 {
		h.cond.Broadcast()
	}
}

// Set rebinds tracked routes to newHandlers. Routes absent from newHandlers
// are kept but serve 404.
func (h *MutableHTTPHandlers) Set(newHandlers map[string]http.Handler) {
	h.lock.Lock()
	defer h.lock.Unlock()

	for path, newHandler := range newHandlers {
		handler := &httpHandler{parent: h}
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

// AsInterface returns the tracked routes as an [http.Handler] map.
func (h *MutableHTTPHandlers) AsInterface() map[string]http.Handler {
	h.lock.Lock()
	defer h.lock.Unlock()

	handlers := make(map[string]http.Handler, len(h.routes))
	for path, handler := range h.routes {
		handlers[path] = handler
	}
	return handlers
}
