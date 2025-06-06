// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"strings"
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
	h.lock.RLock()
	defer h.lock.RUnlock()

	// The :path pseudo-header takes the form of /Prefix/Path
	parsed := strings.Split(r.URL.Path, "/")
	if len(parsed) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	chainID := parsed[1]
	handler, ok := h.handlers[chainID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Deep copy the request to avoid weird behavior from modifying r
	requestDeepCopy := r.Clone(r.Context())
	// Route this request to the http2 handler using the chain prefix
	requestDeepCopy.URL.Path = strings.TrimPrefix(
		requestDeepCopy.URL.Path,
		"/"+chainID,
	)

	handler.ServeHTTP(w, requestDeepCopy)
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
