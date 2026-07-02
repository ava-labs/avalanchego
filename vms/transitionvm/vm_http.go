// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/utils/lock"
)

// httpHandler wraps an [http.Handler], serving 404 when it is nil. New requests
// pass through its parent's gate and are counted as in-flight so they can be
// blocked and drained around a transition.
type httpHandler struct {
	parent *httpHandlers

	lock    sync.RWMutex
	handler http.Handler
}

func (h *httpHandler) set(handler http.Handler) {
	h.lock.Lock()
	defer h.lock.Unlock()

	h.handler = handler
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Park until requests are unblocked. enter returns false only if the
	// request's context is canceled while parked, which here means the
	// connection closed; the client is gone, so there is no response to write.
	if !h.parent.enter(r.Context()) {
		return
	}
	defer h.parent.leave()

	h.lock.RLock()
	handler := h.handler
	h.lock.RUnlock()

	if handler == nil {
		http.NotFound(w, r)
		return
	}

	handler.ServeHTTP(w, r)
}

// httpHandlers is an append-only collection of updatable routes. It can block
// new requests to every route and drain the in-flight ones, so a transition can
// quiesce the active VM's API before shutting it down.
type httpHandlers struct {
	lock sync.Mutex
	// cond is broadcast when blocked changes or inflight goes to 0, waking any
	// waiters in enter and Drain to re-check their conditions.
	cond     *lock.Cond
	routes   map[string]*httpHandler
	blocked  bool // whether new requests are parked
	inflight int  // number of requests currently being served
}

func newHTTPHandlers() *httpHandlers {
	h := &httpHandlers{
		routes: make(map[string]*httpHandler),
	}
	h.cond = lock.NewCond(&h.lock)
	return h
}

// block parks new requests to every route until [httpHandlers.unblock] is
// called. In-flight requests are unaffected.
func (h *httpHandlers) block() {
	h.lock.Lock()
	defer h.lock.Unlock()

	h.blocked = true
}

// unblock lets new requests through again, releasing any parked by
// [httpHandlers.block].
func (h *httpHandlers) unblock() {
	h.lock.Lock()
	defer h.lock.Unlock()

	h.blocked = false
	h.cond.Broadcast()
}

// drain blocks until no requests are in flight or ctx is canceled, returning
// ctx's error in the latter case.
func (h *httpHandlers) drain(ctx context.Context) error {
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
// If it returns true, [httpHandlers.leave] MUST be called.
func (h *httpHandlers) enter(ctx context.Context) bool {
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

// leave records that an in-flight request returned, waking [httpHandlers.drain]
// if none remain.
func (h *httpHandlers) leave() {
	h.lock.Lock()
	defer h.lock.Unlock()

	h.inflight--
	if h.inflight == 0 {
		h.cond.Broadcast()
	}
}

// set rebinds tracked routes to newHandlers. Routes absent from newHandlers
// are kept but serve 404.
func (h *httpHandlers) set(newHandlers map[string]http.Handler) {
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

// asInterface returns the tracked routes as an [http.Handler] map.
func (h *httpHandlers) asInterface() map[string]http.Handler {
	h.lock.Lock()
	defer h.lock.Unlock()

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

	// The engine only calls [VM.CreateHandlers] once. The transitionVM assumes
	// that the routes exposed by the pre-transition VM are a super-set of the
	// routes exposed by the post-transition VM.
	//
	// Coreth registers:
	// - /rpc (always)
	// - /ws (always)
	// - /avax (always)
	// - /admin (sometimes)
	//
	// CChain VM registers:
	// - /rpc (always)
	// - /ws (always)
	// - /avax (always)
	//
	// So Coreth's routes are a super-set of the CChain VM's routes.
	return vm.httpHandlers.asInterface(), nil
}

// None of Subnet-EVM, Coreth, or SAEVM implement NewHTTPHandler, so it is left
// unimplemented.
func (*VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	return nil, nil
}
