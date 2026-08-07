// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubHandler responds with 200 and the path it was registered under.
type stubHandler string

func (s stubHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(s))
}

func stubHandlers(paths ...string) map[string]http.Handler {
	m := make(map[string]http.Handler, len(paths))
	for _, path := range paths {
		m[path] = stubHandler(path)
	}
	return m
}

func get(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec
}

// TestHandlerMap asserts that a [handlerMap] exposes every path registered at
// construction, serving 404 until [handlerMap.setHandlers] provides the actual
// implementations. The toInterface map should be available at any time.
func TestHandlerMap(t *testing.T) {
	paths := []string{"/foo", "/bar", "/baz"}
	m := newHandlerMap(paths...)

	before := m.toInterface()
	require.ElementsMatchf(t, paths, slices.Collect(maps.Keys(before)), "%T.toInterface() paths", m)

	for path, h := range before {
		assert.Equalf(t, http.StatusNotFound, get(t, h).Code, "GET %q before setHandlers", path)
	}

	m.setHandlers(stubHandlers(paths...))

	after := m.toInterface()
	require.ElementsMatchf(t, paths, slices.Collect(maps.Keys(after)), "%T.toInterface() paths", m)

	for desc, handlers := range map[string]map[string]http.Handler{
		"toInterface_before_setHandlers": before,
		"toInterface_after_setHandlers":  after,
	} {
		for path, h := range handlers {
			rec := get(t, h)
			assert.Equalf(t, http.StatusOK, rec.Code, "GET %q via %s", path, desc)
			assert.Equalf(t, path, rec.Body.String(), "GET %q via %s routed to wrong handler", path, desc)
		}
	}
}
