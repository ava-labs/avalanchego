// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/stretchr/testify/require"
)

func TestRejectMiddleware(t *testing.T) {
	type test struct {
		name            string
		handlerFunc     func(*require.Assertions) http.Handler
		contextFunc     func() *snow.ConsensusContext
		expectedErrCode int
	}

	tests := []test{
		{
			name: "chain is bootstrapping",
			handlerFunc: func(require *require.Assertions) http.Handler {
				return http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
					require.Fail("shouldn't have called handler")
				})
			},
			contextFunc: func() *snow.ConsensusContext {
				ctx := &snow.ConsensusContext{}
				ctx.State.Set(snow.EngineState{
					State: snow.Bootstrapping,
				})
				return ctx
			},
			expectedErrCode: http.StatusServiceUnavailable,
		},
		{
			name: "chain is done bootstrapping",
			handlerFunc: func(require *require.Assertions) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusTeapot)
				})
			},
			contextFunc: func() *snow.ConsensusContext {
				ctx := &snow.ConsensusContext{}
				ctx.State.Set(snow.EngineState{
					State: snow.NormalOp,
				})
				return ctx
			},
			expectedErrCode: http.StatusTeapot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			middleware := rejectMiddleware(tt.handlerFunc(require), tt.contextFunc())
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, nil)
			require.Equal(tt.expectedErrCode, w.Code)
		})
	}
}
