// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

// verifyingStub is a minimal SolanaVerifier with an injected rpcClient,
// used to drive the HTTP layer tests without a real Solana RPC server.
func newStubVerifier(rpc rpcClient) *SolanaVerifier {
	return &SolanaVerifier{client: rpc}
}

func buildBody(t *testing.T, msg *oracle.OracleMessage, justification []byte) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{
		"message_bytes": hex.EncodeToString(msg.Bytes()),
		"justification": hex.EncodeToString(justification),
	})
	require.NoError(t, err)
	return b
}

func TestServer_Routing(t *testing.T) {
	msg := makeMsg(t, testProgram, testSlotV, []byte("payload"))
	justification := make([]byte, 64)
	body := buildBody(t, msg, justification)

	tests := []struct {
		name       string
		method     string
		path       string
		body       []byte
		rpc        *stubRPC
		wantStatus int
	}{
		{
			name:       "wrong path returns 404",
			method:     http.MethodPost,
			path:       "/notfound",
			body:       body,
			rpc:        &stubRPC{Tx: makeValidTx(testProgram, testSlotV, []byte("payload"))},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "GET returns 405",
			method:     http.MethodGet,
			path:       "/verify",
			body:       body,
			rpc:        &stubRPC{Tx: makeValidTx(testProgram, testSlotV, []byte("payload"))},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "malformed JSON body returns 400",
			method:     http.MethodPost,
			path:       "/verify",
			body:       []byte("not-json"),
			rpc:        &stubRPC{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid hex message_bytes returns 400",
			method:     http.MethodPost,
			path:       "/verify",
			body:       []byte(`{"message_bytes":"zzzz","justification":""}`),
			rpc:        &stubRPC{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "verifier error returns 400",
			method:     http.MethodPost,
			path:       "/verify",
			body:       body,
			rpc:        &stubRPC{Err: errors.New("rpc down")},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "valid request returns 200",
			method:     http.MethodPost,
			path:       "/verify",
			body:       body,
			rpc:        &stubRPC{Tx: makeValidTx(testProgram, testSlotV, []byte("payload"))},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(newStubVerifier(tt.rpc))
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			srv.ServeHTTP(rec, req)
			require.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
