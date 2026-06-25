// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handler is a trivial [http.Handler] that writes a fixed marker body.
type handler string

func (h handler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(h))
}

// TestHTTPHandlers verifies that a chain transition is reflected through the
// handlers the node captured on startup: shared routes rebind to the new
// handler and dropped routes 404 instead of panicking.
func TestHTTPHandlers(t *testing.T) {
	hs := newHTTPHandlers()
	hs.set(map[string]http.Handler{
		"shared":   handler("pre-shared"),
		"pre-only": handler("pre-only"),
	})
	// The node captures handlers once on startup; we never re-read this map.
	handlers := hs.asInterface()

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
	hs.set(map[string]http.Handler{
		"shared": handler("post-shared"),
	})
	assertRoutes("post-transition", responses{
		"shared":   {wantCode: http.StatusOK, wantBody: "post-shared"},                // rebound
		"pre-only": {wantCode: http.StatusNotFound, wantBody: "404 page not found\n"}, // dropped, now 404s
	})
}
