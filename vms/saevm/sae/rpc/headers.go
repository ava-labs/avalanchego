// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"net/http"
	"strconv"
	"sync"
)

type responseHeadersKey struct{}

// responseHeaders accumulates per-request values that are reported as response
// headers.
//
// A timed-out request is answered by a separate goroutine while the method
// may still be running, so all access is guarded by mu. Values added after the
// headers are written are not reported.
type responseHeaders struct {
	mu     sync.Mutex
	sealed bool
	gas    uint64
	hasGas bool
}

// seal sets the accumulated headers on h. Only the first call has any effect.
func (rh *responseHeaders) seal(h http.Header) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	if rh.sealed {
		return
	}
	rh.sealed = true
	if rh.hasGas {
		h.Set(GasUsedHeader, strconv.FormatUint(rh.gas, 10))
	}
}

// withResponseHeaders wraps a JSON-RPC HTTP handler to set the response headers
// accumulated in [responseHeaders]. It MUST NOT wrap a websocket handler.
func withResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rh := new(responseHeaders)
		ctx := context.WithValue(r.Context(), responseHeadersKey{}, rh)
		next.ServeHTTP(&headerWriter{ResponseWriter: w, headers: rh}, r.WithContext(ctx))
		// JSON-RPC "notifications" run without writing a response, so the
		// headers are still unsent. The libevm RPC server waits for all calls
		// before returning, so no more updates can arrive.
		rh.seal(w.Header())
	})
}

var _ http.ResponseWriter = (*headerWriter)(nil)

// headerWriter sets the headers in [responseHeaders] immediately before the
// response header is written.
type headerWriter struct {
	http.ResponseWriter
	headers *responseHeaders
}

func (w *headerWriter) WriteHeader(code int) {
	w.headers.seal(w.Header())
	w.ResponseWriter.WriteHeader(code)
}

func (w *headerWriter) Write(b []byte) (int, error) {
	w.headers.seal(w.Header())
	return w.ResponseWriter.Write(b)
}

var _ http.Flusher = (*headerWriter)(nil)

// Flush sends any buffered response data to the client by calling Flush on the
// underlying [http.ResponseWriter], if it implements [http.Flusher].
//
// Embedding [http.ResponseWriter] hides the optional [http.Flusher] interface
// of the underlying writer. The libevm RPC server flushes error responses, such
// as timeouts, so that they reach the client before the HTTP server's write
// deadline. Flush doesn't seal because libevm only flushes after
// [headerWriter.Write].
func (w *headerWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap allows [http.ResponseController] to reach the underlying writer.
func (w *headerWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
