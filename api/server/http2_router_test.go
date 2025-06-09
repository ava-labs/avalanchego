// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestHTTP2RouterAdd(t *testing.T) {
	require := require.New(t)
	h := newHTTP2Router()
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	require.True(h.Add(ids.Empty, handler))
	require.False(h.Add(ids.Empty, handler))
}

func TestHTTP2RouterServeHTTP(t *testing.T) {
	tests := []struct {
		name     string
		chainIDs []ids.ID
		header   http.Header
		wantCode int
	}{
		{
			name:     "missing chain-id header",
			wantCode: http.StatusBadRequest,
		},
		{
			name: "unknown referenced chain-id",
			header: http.Header{
				http.CanonicalHeaderKey("chain-id"): []string{ids.Empty.String()},
			},
			wantCode: http.StatusNotFound,
		},
		{
			name:     "valid handler",
			chainIDs: []ids.ID{ids.Empty},
			header: http.Header{
				http.CanonicalHeaderKey("chain-id"): []string{ids.Empty.String()},
			},
			wantCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			h := newHTTP2Router()
			handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
			writer := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			request.Header = tt.header

			for _, chainID := range tt.chainIDs {
				require.True(h.Add(chainID, handler))
			}

			h.ServeHTTP(writer, request)
			require.Equal(tt.wantCode, writer.Code)
		})
	}
}
