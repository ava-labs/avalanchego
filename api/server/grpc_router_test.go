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

func TestGRPCRouterAdd(t *testing.T) {
	require := require.New(t)
	g := newGRPCRouter()
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	require.True(g.Add(ids.Empty, "foo", h))
	require.False(g.Add(ids.Empty, "foo", h))
}

func TestGRPCRouterServeHTTP(t *testing.T) {
	type service struct {
		chainID ids.ID
		service string
	}

	tests := []struct {
		name     string
		services []service
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
			name: "valid handler",
			services: []service{
				{
					chainID: ids.ID{'f', 'o', 'o'},
					service: "bar",
				},
			},
			path:     fmt.Sprintf("/%s/bar/method", ids.ID{'f', 'o', 'o'}.String()),
			wantCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			grpcRouter := newGRPCRouter()
			handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
			writer := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			request.URL = &url.URL{
				Path: tt.path,
			}

			for _, service := range tt.services {
				require.True(grpcRouter.Add(service.chainID, service.service, handler))
			}

			grpcRouter.ServeHTTP(writer, request)
			require.Equal(tt.wantCode, writer.Code)
		})
	}
}
