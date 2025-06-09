// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

var _ http.Handler = (*http2Router)(nil)

type http2Router struct {
	lock     sync.RWMutex
	handlers map[string]http.Handler
}

func newHTTP2Router() *http2Router {
	return &http2Router{
		handlers: make(map[string]http.Handler),
	}
}

func (h *http2Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// the chain-id header must be set to route the request to the correct chain
	// http2 handler
	chainID := r.Header.Get("chain-id")
	if len(chainID) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	h.lock.RLock()
	handler, ok := h.handlers[chainID]
	h.lock.RUnlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	handler.ServeHTTP(w, r)
}

func (h *http2Router) Add(chainID ids.ID, handler http.Handler) bool {
	h.lock.Lock()
	defer h.lock.Unlock()

	chainIDStr := chainID.String()
	if _, ok := h.handlers[chainIDStr]; ok {
		return false
	}

	h.handlers[chainIDStr] = handler
	return true
}
