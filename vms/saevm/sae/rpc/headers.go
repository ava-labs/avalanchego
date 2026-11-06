// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"
)

const (
	// GasUsedHeader is the HTTP response header carrying the total gas used by
	// every eth_call in the request, batches included. It is a decimal integer
	// and is omitted if no eth_call completed.
	GasUsedHeader = "Gas-Used"
	// ErrorCodesHeader is the HTTP response header listing each JSON-RPC error
	// code in the response with the number of times it occurred, as an RFC 9651
	// list sorted by code, e.g. `-32002;count=1, 3;count=2`. It is omitted if no
	// response has an error.
	ErrorCodesHeader = "Rpc-Error-Codes"
)

type responseHeadersKey struct{}

// responseHeaders accumulates per-request values that are reported as response
// headers.
//
// A timed-out request is answered by a separate goroutine while the method
// may still be running, so all access is guarded by mu. Values added after the
// headers are written are not reported, because changes to an [http.Header]
// after the response is written have no effect.
type responseHeaders struct {
	mu     sync.Mutex
	gas    uint64
	hasGas bool
}

// setHeaders sets the accumulated headers on h, along with errorCodes as counted
// by [parseErrorCodes]. It has no effect if the response has already been
// written.
func (rh *responseHeaders) setHeaders(h http.Header, errorCodes map[int]int) {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	if rh.hasGas {
		h.Set(GasUsedHeader, strconv.FormatUint(rh.gas, 10))
	}
	if v := formatErrorCodesHeader(errorCodes); v != "" {
		h.Set(ErrorCodesHeader, v)
	}
}

// formatErrorCodesHeader formats counts as the value of [ErrorCodesHeader].
func formatErrorCodesHeader(counts map[int]int) string {
	items := make([]string, 0, len(counts))
	for _, code := range slices.Sorted(maps.Keys(counts)) {
		items = append(items, strconv.Itoa(code)+";count="+strconv.Itoa(counts[code]))
	}
	return strings.Join(items, ", ")
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
	rh.gas += gas
	rh.hasGas = true
}

// Call overrides [ethapi.BlockChainAPI.Call] to record the gas used for
// [GasUsedHeader]. The interceptor needs the request's context, so it is set
// on a [ethapi.BlockChainAPI] built for each call.
func (b *blockChainAPI) Call(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *ethapi.StateOverride, blockOverrides *ethapi.BlockOverrides) (hexutil.Bytes, error) {
	recordGas := func(r *core.ExecutionResult) error {
		addGas(ctx, r.UsedGas)
		return nil
	}
	api := ethapi.NewBlockChainAPI(b.b, ethapi.WithDefaultCallOptions(ethapi.WithCallResultInterceptor(recordGas)))
	return api.Call(ctx, args, blockNrOrHash, overrides, blockOverrides)
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
		// before returning, so no more updates can arrive. If a response was
		// written, this has no effect.
		rh.setHeaders(w.Header(), nil)
	})
}

var _ http.ResponseWriter = (*headerWriter)(nil)

// headerWriter sets the headers in [responseHeaders] immediately before the
// response header is written. The libevm RPC server writes each response,
// batches included, in a single call to Write.
type headerWriter struct {
	http.ResponseWriter
	headers *responseHeaders
}

func (w *headerWriter) WriteHeader(code int) {
	w.headers.setHeaders(w.Header(), nil)
	w.ResponseWriter.WriteHeader(code)
}

func (w *headerWriter) Write(b []byte) (int, error) {
	w.headers.setHeaders(w.Header(), parseErrorCodes(b))
	return w.ResponseWriter.Write(b)
}

// parseErrorCodes returns the number of times each JSON-RPC error code occurs
// in body, which holds either a single response or a batch. Malformed input is
// ignored.
//
// TODO(SayanKar): a response has either an error or a result, and the libevm
// RPC server encodes the error first, so decoding a single response could stop
// at whichever comes first instead of scanning a potentially large result, if
// decoding large responses significantly increases latency.
func parseErrorCodes(body []byte) map[int]int {
	// Most responses have no error, so they skip decoding. A result that
	// happens to contain the key is still decoded in full.
	if !bytes.Contains(body, []byte(`"error"`)) {
		return nil
	}

	type response struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}

	// A single response is decoded as a batch of one. Like the libevm RPC
	// server, a batch is recognised by its first non-whitespace byte.
	batch := make([]response, 1)
	var dst any = &batch[0]
	if bytes.HasPrefix(bytes.TrimLeft(body, " \t\r\n"), []byte("[")) {
		dst = &batch
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return nil
	}

	counts := make(map[int]int)
	for _, r := range batch {
		if r.Error != nil {
			counts[r.Error.Code]++
		}
	}
	return counts
}

var _ http.Flusher = (*headerWriter)(nil)

// Flush sends any buffered response data to the client by calling Flush on the
// underlying [http.ResponseWriter], if it implements [http.Flusher].
//
// Embedding [http.ResponseWriter] hides the optional [http.Flusher] interface
// of the underlying writer. The libevm RPC server flushes error responses, such
// as timeouts, so that they reach the client before the HTTP server's write
// deadline. Flush doesn't set the headers because libevm only flushes after
// [headerWriter.Write].
func (w *headerWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
