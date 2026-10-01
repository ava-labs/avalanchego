// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"net/http"
	"strconv"
	"sync"
)

// GasUsedHeader is the HTTP response header carrying the total gas used by
// every eth_call in the request, batches included. It is a decimal integer and
// is omitted if no eth_call executed.
const GasUsedHeader = "Gas-Used"

// ErrorCodesHeader is the HTTP response header listing each JSON-RPC error
// code in the response with the number of times it occurred, as an RFC 8941
// list sorted by code, e.g. `-32002;count=1, 3;count=2`. It is omitted if no
// response has an error.
const ErrorCodesHeader = "Rpc-Errors"

type responseHeadersKey struct{}

// responseHeaders accumulates per-request values that are reported as response
// headers.
//
// A timed-out request is answered by a separate goroutine while the method
// may still be running, so all access is guarded by mu and updates after the
// headers are written are dropped.
type responseHeaders struct {
	mu     sync.Mutex
	sealed bool
	gas    uint64
	hasGas bool
}

// addGas records gas used by a call in the request carried by ctx. It is a
// no-op if ctx was not created by [withResponseHeaders].
func addGas(ctx context.Context, gas uint64) {
	rh, ok := ctx.Value(responseHeadersKey{}).(*responseHeaders)
	if !ok {
		return
	}
	rh.mu.Lock()
	defer rh.mu.Unlock()
	if rh.sealed {
		return
	}
	rh.gas += gas
	rh.hasGas = true
}

// seal stops further updates and sets the accumulated headers on h. Error
// codes are read from body, which MUST be either empty or the entire response
// body. Only the first call has any effect.
func (rh *responseHeaders) seal(h http.Header, body []byte) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	if rh.sealed {
		return
	}
	rh.sealed = true
	if rh.hasGas {
		h.Set(GasUsedHeader, strconv.FormatUint(rh.gas, 10))
	}
	if v := formatErrorCodesHeader(parseErrorCodes(body)); v != "" {
		h.Set(ErrorCodesHeader, v)
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
		rh.seal(w.Header(), nil)
	})
}

var (
	_ http.ResponseWriter = (*headerWriter)(nil)
	_ http.Flusher        = (*headerWriter)(nil)
)

// headerWriter sets the headers in [responseHeaders] immediately before the
// response header is written. The libevm RPC server writes each response,
// batches included, in a single call to Write.
type headerWriter struct {
	http.ResponseWriter
	headers *responseHeaders
}

func (w *headerWriter) WriteHeader(code int) {
	w.headers.seal(w.Header(), nil)
	w.ResponseWriter.WriteHeader(code)
}

func (w *headerWriter) Write(b []byte) (int, error) {
	w.headers.seal(w.Header(), b)
	return w.ResponseWriter.Write(b)
}

// Flush implements [http.Flusher], which the libevm RPC server relies on when
// writing error responses. It doesn't seal because libevm only flushes after
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
