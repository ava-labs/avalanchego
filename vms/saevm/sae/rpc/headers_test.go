// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeaderNamesCanonical(t *testing.T) {
	require.Equal(t, GasUsedHeader, http.CanonicalHeaderKey(GasUsedHeader))
}

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
			name: "set_on_flush",
			handler: func(w http.ResponseWriter, r *http.Request) {
				addGas(r.Context(), 1)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
			withResponseHeaders(tt.handler).ServeHTTP(rec, req)

			require.Equalf(t, tt.want, rec.Result().Header[GasUsedHeader], "%q header", GasUsedHeader)
		})
	}
}
