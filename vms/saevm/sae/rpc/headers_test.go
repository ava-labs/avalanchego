// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithResponseHeaders(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    []string // nil if the header must be absent
	}{
		{
			name: "no_gas",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("{}"))
			},
		},
		{
			name: "summed",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 21000)
				addGas(r.Context(), 0)
				addGas(r.Context(), 2675)
				_, _ = w.Write([]byte("{}"))
			},
			want: []string{"23675"},
		},
		{
			name: "zero_gas_reported",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 0)
				_, _ = w.Write([]byte("{}"))
			},
			want: []string{"0"},
		},
		{
			name: "set_on_write_header",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 1)
				w.WriteHeader(http.StatusOK)
			},
			want: []string{"1"},
		},
		{
			name: "set_on_write_then_flush",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 1)
				_, _ = w.Write([]byte("{}"))
				w.(http.Flusher).Flush()
			},
			want: []string{"1"},
		},
		{
			name: "dropped_after_write",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 1)
				_, _ = w.Write([]byte("{"))
				addGas(r.Context(), 2)
				_, _ = w.Write([]byte("}"))
			},
			want: []string{"1"},
		},
		{
			name: "set_without_write",
			handler: func(_ http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 1)
			},
			want: []string{"1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
			withResponseHeaders(tt.handler).ServeHTTP(rec, req)

			require.Equalf(t, tt.want, rec.Result().Header.Values(GasUsedHeader), "%q header", GasUsedHeader)
		})
	}
}

func TestErrorCodesHeader(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string // nil if the header must be absent
	}{
		{
			name: "no_body",
		},
		{
			name: "single_result",
			body: `{"jsonrpc":"2.0","id":1,"result":"0x1"}`,
		},
		{
			name: "single_error",
			body: `{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"execution reverted","data":"0x2a"}}` + "\n",
			want: []string{"3;count=1"},
		},
		{
			name: "single_null_error",
			body: `{"jsonrpc":"2.0","id":1,"error":null,"result":"0x1"}`,
		},
		{
			name: "error_inside_result",
			body: `{"id":1,"result":{"error":{"code":-1}}}`,
		},
		{
			name: "batch",
			body: `[` +
				`{"jsonrpc":"2.0","id":1,"result":{"error":{"code":-1}}},` +
				`{"jsonrpc":"2.0","id":2,"error":{"code":-32002,"message":"request timed out"}},` +
				`{"jsonrpc":"2.0","id":3,"error":{"code":3,"message":"execution reverted"}},` +
				`{"jsonrpc":"2.0","id":4,"error":{"code":3,"message":"execution reverted"}}` +
				`]`,
			want: []string{"-32002;count=1, 3;count=2"},
		},
		{
			name: "batch_leading_whitespace",
			body: " \r\n\t[" + `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"x"}}]`,
			want: []string{"-32601;count=1"},
		},
		{
			name: "batch_no_errors",
			body: `[{"jsonrpc":"2.0","id":1,"result":"0x1"}]`,
		},
		{
			name: "malformed",
			body: `[{"jsonrpc":"2.0","id":1,"error":{"code":3}`,
		},
		{
			name: "not_json",
			body: "method not allowed\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, _ *http.Request) {
				if tt.body != "" {
					_, _ = w.Write([]byte(tt.body))
				}
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
			withResponseHeaders(http.HandlerFunc(handler)).ServeHTTP(rec, req)

			require.Equalf(t, tt.want, rec.Result().Header.Values(ErrorCodesHeader), "%q header", ErrorCodesHeader)
		})
	}
}
