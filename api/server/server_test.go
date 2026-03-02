// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

func TestRejectMiddleware(t *testing.T) {
	type test struct {
		name               string
		handler            http.Handler
		state              snow.State
		expectedStatusCode int
	}

	tests := []test{
		{
			name:               "chain is state syncing",
			state:              snow.StateSyncing,
			expectedStatusCode: http.StatusServiceUnavailable,
		},
		{
			name:               "chain is bootstrapping",
			state:              snow.Bootstrapping,
			expectedStatusCode: http.StatusServiceUnavailable,
		},
		{
			name: "chain is done bootstrapping",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}),
			state:              snow.NormalOp,
			expectedStatusCode: http.StatusTeapot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			snowCtx := snowtest.Context(t, snowtest.CChainID)
			ctx := snowtest.ConsensusContext(snowCtx)
			ctx.State.Set(snow.EngineState{
				State: tt.state,
			})

			middleware := rejectMiddleware(tt.handler, ctx)
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, nil)
			require.Equal(tt.expectedStatusCode, w.Code)
		})
	}
}

func TestHTTPHeaderRouteIsCanonical(t *testing.T) {
	wantHeaderKey := http.CanonicalHeaderKey(HTTPHeaderRoute)
	require.Equal(t, wantHeaderKey, HTTPHeaderRoute)
}
