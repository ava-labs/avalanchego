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
		name           string
		handler        http.HandlerFunc
		wantGas        []string // nil if the header must be absent
		wantErrorCodes []string
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
			wantGas: []string{"23675"},
		},
		{
			name: "zero_gas_reported",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 0)
				_, _ = w.Write([]byte("{}"))
			},
			wantGas: []string{"0"},
		},
		{
			name: "set_on_write_header",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 1)
				w.WriteHeader(http.StatusOK)
			},
			wantGas: []string{"1"},
		},
		{
			name: "set_on_write_then_flush",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 1)
				_, _ = w.Write([]byte("{}"))
				w.(http.Flusher).Flush()
			},
			wantGas: []string{"1"},
		},
		{
			name: "dropped_after_write",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 1)
				_, _ = w.Write([]byte("{"))
				addGas(r.Context(), 2)
				_, _ = w.Write([]byte("}"))
			},
			wantGas: []string{"1"},
		},
		{
			name: "error_codes",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"x"}}`))
			},
			wantErrorCodes: []string{"-32000;count=1"},
		},
		{
			name: "set_without_write",
			handler: func(_ http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 1)
			},
			wantGas: []string{"1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
			withResponseHeaders(tt.handler).ServeHTTP(rec, req)

			got := rec.Result().Header
			require.Equalf(t, tt.wantGas, got.Values(GasUsedHeader), "%q header", GasUsedHeader)
			require.Equalf(t, tt.wantErrorCodes, got.Values(ErrorCodesHeader), "%q header", ErrorCodesHeader)
		})
	}
}
