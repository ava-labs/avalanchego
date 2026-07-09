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

// recordNewResponse dispatches a request to h and returns the recorded
// response.
func recordNewResponse(h http.Handler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	return w
}

// TestHTTPHandlers verifies a transition is reflected through the handlers
// captured on startup: shared routes rebind and dropped routes 404.
func TestHTTPHandlers(t *testing.T) {
	sut := newSUT(t, 1)
	ctx := t.Context()

	const (
		sharedPath     = "shared"
		sharedBodyPre  = "pre-shared"
		sharedBodyPost = "post-shared"

		preOnlyPath = "pre-only"
		preOnlyBody = "pre-only"
	)
	sut.pre.handlers = map[string]http.Handler{
		sharedPath:  handler(sharedBodyPre),
		preOnlyPath: handler(preOnlyBody),
	}
	sut.post.handlers = map[string]http.Handler{
		sharedPath: handler(sharedBodyPost),
	}

	// The node captures the handler map once on startup and never re-reads it.
	handlers, err := sut.CreateHandlers(ctx)
	require.NoErrorf(t, err, "%T.CreateHandlers()", sut.VM)

	type responses map[string]struct {
		wantCode int
		wantBody string
	}
	assertRoutes := func(phase string, want responses) {
		t.Helper()

		for path, want := range want {
			h, ok := handlers[path]
			require.Truef(t, ok, "%s: %s handler exists", phase, path)

			rec := recordNewResponse(h)
			assert.Equalf(t, want.wantCode, rec.Code, "%s: %s response code", phase, path)
			assert.Equalf(t, want.wantBody, rec.Body.String(), "%s: %s response body", phase, path)
		}
	}

	assertRoutes("pre-transition", responses{
		sharedPath:  {wantCode: http.StatusOK, wantBody: sharedBodyPre},
		preOnlyPath: {wantCode: http.StatusOK, wantBody: preOnlyBody},
	})

	sut.BuildVerifyAccept(t, ctx, noContext)

	assertRoutes("post-transition", responses{
		sharedPath:  {wantCode: http.StatusOK, wantBody: sharedBodyPost},               // rebound
		preOnlyPath: {wantCode: http.StatusNotFound, wantBody: "404 page not found\n"}, // dropped, now 404s
	})
}

// TestHTTPHandlersBlockUnblock verifies that Block parks new requests until
// Unblock releases them, at which point they serve whatever handler is then
// installed, while in-flight requests are untouched.
func TestHTTPHandlersBlockUnblock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		handlers := newHTTPHandlers()
		const (
			path = "path"
			body = "body"
		)
		handlers.set(map[string]http.Handler{
			path: handler(body),
		})
		handler := handlers.routes[path]

		// Served normally before block.
		require.Equalf(t, body, recordNewResponse(handler).Body.String(), "serve(%T)", handler)

		handlers.block()

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
		handlers.unblock()
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
		handlers := newHTTPHandlers()
		const path = "path"
		handlers.set(map[string]http.Handler{
			path: blockingHandler{release: release},
		})
		route := handlers.routes[path]

		// Draining with no in-flight requests returns immediately.
		require.NoErrorf(t, handlers.drain(t.Context()), "%T.drain()", handlers)

		// Draining with in-flight requests blocks until the context is
		// cancelled.
		go recordNewResponse(route)
		synctest.Wait() // The request is now in flight, blocked in the handler.

		ctx, cancel := context.WithCancel(t.Context())
		testUnblocking(t,
			func(t *testing.T) {
				require.ErrorIsf(t, handlers.drain(ctx), context.Canceled, "%T.drain() with cancelled context")
			},
			cancel,
		)

		testUnblocking(t,
			func(t *testing.T) {
				require.NoErrorf(t, handlers.drain(t.Context()), "%T.drain() after final in-flight request")
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

// TestTransitionAbandonsStuckAPIRequests verifies a transition proceeds past
// the drain timeout while an API request is stuck in the pre-transition
// chain, and that a request parked during the transition is served by the
// post-transition chain.
func TestTransitionAbandonsStuckAPIRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sut := newSUT(t, 1)
		ctx := t.Context()

		release := make(chan struct{})
		defer close(release)

		const (
			path     = "path"
			bodyPost = "post"
		)
		sut.pre.handlers = map[string]http.Handler{
			path: blockingHandler{release: release},
		}
		sut.post.handlers = map[string]http.Handler{
			path: handler(bodyPost),
		}

		handlers, err := sut.CreateHandlers(ctx)
		require.NoErrorf(t, err, "%T.CreateHandlers()", sut.VM)
		route := handlers[path]

		go recordNewResponse(route)
		synctest.Wait() // The request is in flight, blocked in the handler.

		// Accepting the transition block blocks new API requests and waits for
		// the stuck one until the drain timeout.
		blk, err := sut.BuildBlock(ctx)
		require.NoErrorf(t, err, "%T.BuildBlock()", sut.VM)
		require.NoErrorf(t, blk.Verify(ctx), "%T.Verify()", blk)
		go func() {
			assert.NoErrorf(t, blk.Accept(ctx), "%T.Accept()", blk)
		}()
		synctest.Wait() // The transition is now waiting on the drain.

		// The parked request is served by the post-transition chain after
		// abandoning the stuck request.
		require.Equalf(t, bodyPost, recordNewResponse(route).Body.String(), "serve(%T)", route)
	})
}
