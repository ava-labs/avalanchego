// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestGRPCRouterServeHTTP(t *testing.T) {
	tests := []struct {
		name     string
		chainIDs []ids.ID
		path     string
		wantCode int
	}{
		{
			name:     "invalid request",
			path:     "foo",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid handler",
			path:     "/foo/bar/method",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "valid handler",
			chainIDs: []ids.ID{{'f', 'o', 'o'}},
			path:     fmt.Sprintf("/%s/bar/method", ids.ID{'f', 'o', 'o'}.String()),
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
			request.URL = &url.URL{
				Path: tt.path,
			}

			for _, chainID := range tt.chainIDs {
				require.True(h.Add(chainID, handler))
			}

			h.ServeHTTP(writer, request)
			require.Equal(tt.wantCode, writer.Code)
		})
	}
}
