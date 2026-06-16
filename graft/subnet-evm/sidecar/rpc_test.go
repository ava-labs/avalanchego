// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sidecar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/external"
)

const testTxSig = "5oBBwsRKNicfxqQnGnVwZCmCXtRCpkpxFSJBLRc7nHJ3XcC6nTgxj6HHpABcDeFgH"

func buildEvent(t *testing.T, justification string) *external.ExternalEvent {
	t.Helper()
	msg, err := external.NewExternalMessage("solana", "SomeProgram1111", []byte{0x01}, 42_000, []byte("payload"))
	require.NoError(t, err)
	return &external.ExternalEvent{
		Message:       msg,
		Justification: []byte(justification),
	}
}

// successResponse returns a JSON-RPC response for a transaction that succeeded
// at the given slot.
func successResponse(slot uint64) solanaGetTransactionResponse {
	errVal := interface{}(nil)
	return solanaGetTransactionResponse{
		JSONRPC: "2.0",
		ID:      1,
		Result: &solanaTransactionResult{
			Slot: slot,
			Meta: &solanaMetaResult{
				Err:         errVal,
				LogMessages: []string{"Program log: ok"},
			},
		},
	}
}

func rpcServer(t *testing.T, responder func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		responder(w)
	}))
}

func ruleFor(t *testing.T, serverURL string) *SolanaRPCValidationRule {
	t.Helper()
	return newSolanaRPCValidationRuleWithClient(serverURL, "finalized", &http.Client{})
}

func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func TestRPC_TransactionNotFound(t *testing.T) {
	srv := rpcServer(t, func(w http.ResponseWriter) {
		writeJSON(t, w, solanaGetTransactionResponse{JSONRPC: "2.0", ID: 1, Result: nil})
	})
	defer srv.Close()

	ok, err := ruleFor(t, srv.URL).Validate(t.Context(), buildEvent(t, testTxSig))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRPC_SlotMismatch(t *testing.T) {
	srv := rpcServer(t, func(w http.ResponseWriter) {
		writeJSON(t, w, successResponse(99_999))
	})
	defer srv.Close()

	ok, err := ruleFor(t, srv.URL).Validate(t.Context(), buildEvent(t, testTxSig))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRPC_TransactionFailedOnChain(t *testing.T) {
	const slot uint64 = 42_000
	srv := rpcServer(t, func(w http.ResponseWriter) {
		writeJSON(t, w, solanaGetTransactionResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result: &solanaTransactionResult{
				Slot: slot,
				Meta: &solanaMetaResult{
					Err: "InstructionError",
				},
			},
		})
	})
	defer srv.Close()

	ok, err := ruleFor(t, srv.URL).Validate(t.Context(), buildEvent(t, testTxSig))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRPC_NilMeta(t *testing.T) {
	const slot uint64 = 42_000
	srv := rpcServer(t, func(w http.ResponseWriter) {
		writeJSON(t, w, solanaGetTransactionResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  &solanaTransactionResult{Slot: slot, Meta: nil},
		})
	})
	defer srv.Close()

	ok, err := ruleFor(t, srv.URL).Validate(t.Context(), buildEvent(t, testTxSig))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRPC_HTTPError(t *testing.T) {
	srv := rpcServer(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()

	ok, err := ruleFor(t, srv.URL).Validate(t.Context(), buildEvent(t, testTxSig))
	require.ErrorContains(t, err, "RPC returned HTTP")
	require.False(t, ok)
}

func TestRPC_MalformedJSON(t *testing.T) {
	srv := rpcServer(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte("{not valid json"))
	})
	defer srv.Close()

	ok, err := ruleFor(t, srv.URL).Validate(t.Context(), buildEvent(t, testTxSig))
	require.ErrorContains(t, err, "decode RPC response")
	require.False(t, ok)
}

func TestRPC_RPCErrorField(t *testing.T) {
	srv := rpcServer(t, func(w http.ResponseWriter) {
		writeJSON(t, w, solanaGetTransactionResponse{
			JSONRPC:  "2.0",
			ID:       1,
			RPCError: &solanaRPCError{Code: -32600, Message: "Invalid request"},
		})
	})
	defer srv.Close()

	ok, err := ruleFor(t, srv.URL).Validate(t.Context(), buildEvent(t, testTxSig))
	require.ErrorContains(t, err, "RPC error")
	require.False(t, ok)
}

func TestRPC_EmptyJustification(t *testing.T) {
	srv := rpcServer(t, func(w http.ResponseWriter) {
		writeJSON(t, w, successResponse(42_000))
	})
	defer srv.Close()

	event := buildEvent(t, "")
	ok, err := ruleFor(t, srv.URL).Validate(t.Context(), event)
	require.ErrorContains(t, err, "justification is empty")
	require.False(t, ok)
}

func TestRPC_ServerUnreachable(t *testing.T) {
	// Use a server that's immediately closed.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	ok, err := ruleFor(t, srv.URL).Validate(t.Context(), buildEvent(t, testTxSig))
	require.ErrorContains(t, err, "RPC request")
	require.False(t, ok)
}
