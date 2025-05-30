// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TODO refactor this package so we can test against the actual package api
func TestGRPCRouterAdd(t *testing.T) {
	require := require.New(t)
	g := newGRPCRouter()
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	require.True(g.Add("foo", h))
	require.False(g.Add("foo", h))
}

func TestGRPCRouterServeHTTP(t *testing.T) {
	tests := []struct {
		name     string
		handlers []string
		uri      string
		wantCode int
	}{
		{
			name:     "invalid request",
			uri:      "foobar",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid handler",
			uri:      "/foo/method",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "valid handler",
			handlers: []string{"foo"},
			uri:      "/foo/method",
			wantCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			grpcRouter := newGRPCRouter()
			handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
			writer := httptest.NewRecorder()
			request := httptest.NewRequest("", "/", nil)

			request.RequestURI = tt.uri

			for _, h := range tt.handlers {
				require.True(grpcRouter.Add(h, handler))
			}

			grpcRouter.ServeHTTP(writer, request)
			require.Equal(tt.wantCode, writer.Code)
		})
	}
}
