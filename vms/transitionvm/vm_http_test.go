// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// serve dispatches a request to h and returns the recorded response.
func serve(h http.Handler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	return w
}

// TestHTTPHandlers verifies a transition is reflected through the handlers
// captured on startup: shared routes rebind and dropped routes 404.
func TestHTTPHandlers(t *testing.T) {
	sut := newSUT(t)
	ctx := t.Context()

	sut.pre.handlers = map[string]http.Handler{
		"shared":   handler("pre-shared"),
		"pre-only": handler("pre-only"),
	}
	sut.post.handlers = map[string]http.Handler{
		"shared": handler("post-shared"),
	}

	// The node captures the handler map once on startup and never re-reads it.
	handlers, err := sut.CreateHandlers(ctx)
	require.NoErrorf(t, err, "%T.CreateHandlers()", sut)

	type responses map[string]struct {
		wantCode int
		wantBody string
	}
	assertRoutes := func(phase string, want responses) {
		t.Helper()

		for path, want := range want {
			h, ok := handlers[path]
			require.Truef(t, ok, "%s: %s", phase, path)

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			h.ServeHTTP(w, r)
			assert.Equalf(t, want.wantCode, w.Code, "%s: %s", phase, path)
			assert.Equalf(t, want.wantBody, w.Body.String(), "%s: %s", phase, path)
		}
	}

	assertRoutes("pre-transition", responses{
		"shared":   {wantCode: http.StatusOK, wantBody: "pre-shared"},
		"pre-only": {wantCode: http.StatusOK, wantBody: "pre-only"},
	})

	sut.BuildVerifyAccept(t, ctx, noContext) // triggers the transition

	assertRoutes("post-transition", responses{
		"shared":   {wantCode: http.StatusOK, wantBody: "post-shared"},                // rebound
		"pre-only": {wantCode: http.StatusNotFound, wantBody: "404 page not found\n"}, // dropped, now 404s
	})
}

// TestHTTPHandlersBlockUnblock verifies that Block parks new requests until
// Unblock releases them, at which point they serve whatever handler is then
// installed, while in-flight requests are untouched.
func TestHTTPHandlersBlockUnblock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		handlers := newHTTPHandlers()
		handlers.set(map[string]http.Handler{"rpc": handler("val")})
		route := handlers.routes["rpc"]

		// Served normally before block.
		require.Equalf(t, "val", serve(route).Body.String(), "serve(%T)", route)

		handlers.block()

		// Cancelled requests exit gracefully while blocked.
		ctx, cancel := context.WithCancel(t.Context())
		go cancel()
		route.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx),
		)

		// The request parks while blocked instead of being served.
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			result <- serve(route)
		}()
		synctest.Wait()
		require.Emptyf(t, result, "serve(%T) was served while blocked", route)
		handlers.unblock()
		require.Equalf(t, "val", (<-result).Body.String(), "serve(%T)", route)

		// Served normally after unblock.
		require.Equalf(t, "val", serve(route).Body.String(), "serve(%T)", route)
	})
}

// TestHTTPHandlersDrain verifies that Drain blocks until the in-flight requests
// have returned, and returns immediately when none are in flight.
func TestHTTPHandlersDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		handlers := newHTTPHandlers()
		handlers.set(map[string]http.Handler{"rpc": blockingHandler{release: release}})
		route := handlers.routes["rpc"]

		// Draining with no in-flight requests returns immediately.
		require.NoErrorf(t, handlers.drain(t.Context()), "%T.drain()", handlers)

		// Draining with in-flight requests blocks until the context is
		// cancelled.
		go serve(route)
		synctest.Wait() // The request is now in flight, blocked in the handler.
		cancelledCtx, cancel := context.WithCancel(t.Context())
		go cancel()
		require.ErrorIsf(t, handlers.drain(cancelledCtx), context.Canceled, "%T.drain()", handlers)

		// Draining with in-flight requests blocks until the request returns.
		go close(release)
		require.NoErrorf(t, handlers.drain(t.Context()), "%T.drain()", handlers)
	})
}
