// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handler is an [http.Handler] that writes a fixed body.
type handler string

func (h handler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(h))
}

// blockingHandler is an [http.Handler] that blocks in ServeHTTP until release
// is closed.
type blockingHandler struct {
	release chan struct{}
}

func (h blockingHandler) ServeHTTP(http.ResponseWriter, *http.Request) {
	<-h.release
}

// recordNewResponse dispatches a request to h and returns the recorded
// response.
func recordNewResponse(h http.Handler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	return w
}

// TestHTTPHandlersBlockUnblock verifies that Block parks new requests until
// Unblock releases them, at which point they serve whatever handler is then
// installed, while in-flight requests are untouched.
func TestHTTPHandlersBlockUnblock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		handlers := NewMutableHTTPHandlers()
		const (
			path = "path"
			body = "body"
		)
		handlers.Set(map[string]http.Handler{
			path: handler(body),
		})
		handler := handlers.routes[path]

		// Served normally before block.
		require.Equalf(t, body, recordNewResponse(handler).Body.String(), "serve(%T)", handler)

		handlers.Block()

		// Cancelled requests exit gracefully while blocked.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		handler.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx),
		)

		// The request parks while blocked instead of being served.
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			result <- recordNewResponse(handler)
		}()
		synctest.Wait()
		require.Emptyf(t, result, "serve(%T) was served while blocked", handler)
		handlers.Unblock()
		require.Equalf(t, body, (<-result).Body.String(), "serve(%T)", handler)

		// Served normally after unblock.
		require.Equalf(t, body, recordNewResponse(handler).Body.String(), "serve(%T)", handler)
	})
}

// TestHTTPHandlersDrain verifies that Drain blocks until the in-flight requests
// have returned, and returns immediately when none are in flight.
func TestHTTPHandlersDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		handlers := NewMutableHTTPHandlers()
		const path = "path"
		handlers.Set(map[string]http.Handler{
			path: blockingHandler{release: release},
		})
		route := handlers.routes[path]

		// Draining with no in-flight requests returns immediately.
		require.NoErrorf(t, handlers.Drain(t.Context()), "%T.drain()", handlers)

		// Draining with in-flight requests blocks until the context is
		// cancelled.
		go recordNewResponse(route)
		synctest.Wait() // The request is now in flight, blocked in the handler.

		ctx, cancel := context.WithCancel(t.Context())
		testUnblocking(t,
			func(t *testing.T) {
				require.ErrorIsf(t, handlers.Drain(ctx), context.Canceled, "%T.drain() with cancelled context", handlers)
			},
			cancel,
		)

		testUnblocking(t,
			func(t *testing.T) {
				require.NoErrorf(t, handlers.Drain(t.Context()), "%T.drain() after final in-flight request", handlers)
			},
			func() {
				close(release)
			},
		)
	})
}

// testUnblocking first calls blocking, after it is blocked unblock is called.
func testUnblocking(t *testing.T, blocking func(*testing.T), unblock func()) {
	t.Helper()
	go func() {
		synctest.Wait()
		unblock()
	}()
	blocking(t)
}

// TestHandlerMap asserts that [MutableHTTPHandlers] exposes every path registered at
// construction, serving 404 until [MutableHTTPHandlers.Set] provides the actual
// implementations. The AsInterface map should be available at any time.
func TestHandlerMap(t *testing.T) {
	paths := []string{"/foo", "/bar", "/baz"}
	handlers := NewMutableHTTPHandlers(paths...)

	before := handlers.AsInterface()
	require.ElementsMatchf(t, paths, slices.Collect(maps.Keys(before)), "%T.toInterface() paths", handlers)

	for path, h := range before {
		assert.Equalf(t, http.StatusNotFound, recordNewResponse(h).Code, "GET %q before setHandlers", path)
	}

	newHandlers := make(map[string]http.Handler, len(paths))
	for _, path := range paths {
		newHandlers[path] = handler(path)
	}
	handlers.Set(newHandlers)

	after := handlers.AsInterface()
	require.ElementsMatchf(t, paths, slices.Collect(maps.Keys(after)), "%T.toInterface() paths", handlers)

	for desc, handlers := range map[string]map[string]http.Handler{
		"toInterface_before_setHandlers": before,
		"toInterface_after_setHandlers":  after,
	} {
		for path, h := range handlers {
			rec := recordNewResponse(h)
			assert.Equalf(t, http.StatusOK, rec.Code, "GET %q via %s", path, desc)
			assert.Equalf(t, path, rec.Body.String(), "GET %q via %s routed to wrong handler", path, desc)
		}
	}
}
