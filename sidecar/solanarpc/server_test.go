// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

const (
	// testSourceAddress is a well-known Solana program ID used in tests.
	testSourceAddress = "So11111111111111111111111111111111111111112"
	testSlot          = uint64(100)
)

// makeMockRPC creates an HTTP handler that returns the given value as the
// JSON-RPC result field. Pass nil for a "transaction not found" response.
func makeMockRPC(t *testing.T, result any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		// Verify the incoming request is a getTransaction call.
		var req rpcEnvelope
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "getTransaction", req.Method)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
	}
}

// setupTest wires up:
//   - a mock Solana RPC httptest server driven by rpcHandler
//   - a SolanaVerifier pointing at that mock
//   - a Server wrapping the verifier
//   - a httptest server for the Server
//
// It returns the sidecar server URL, a sample OracleMessage, and a 64-byte
// justification (raw Ed25519 signature bytes).
func setupTest(t *testing.T, rpcHandler http.HandlerFunc) (serverURL string, msg *oracle.OracleMessage, justification []byte) {
	t.Helper()

	mockRPC := httptest.NewServer(rpcHandler)
	t.Cleanup(mockRPC.Close)

	verifier := NewSolanaVerifier(mockRPC.URL, mockRPC.Client())

	srv := NewServer(verifier)
	sidecar := httptest.NewServer(srv)
	t.Cleanup(sidecar.Close)

	var err error
	msg, err = oracle.NewOracleMessage(
		"solana",
		testSourceAddress,
		[]byte{0xde, 0xad, 0xbe, 0xef},
		testSlot,
		1,
		[]byte("payload"),
	)
	require.NoError(t, err)

	// 64-byte justification that acts as a raw Ed25519 signature.
	justification = make([]byte, 64)
	for i := range justification {
		justification[i] = byte(i)
	}

	return sidecar.URL, msg, justification
}

// doVerify sends a POST /verify to the sidecar and returns the HTTP status code.
func doVerify(t *testing.T, serverURL string, msg *oracle.OracleMessage, justification []byte) int {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"message_bytes": hex.EncodeToString(msg.Bytes()),
		"justification": hex.EncodeToString(justification),
	})
	require.NoError(t, err)

	resp, err := http.Post(serverURL+"/verify", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestVerify_ValidTransaction(t *testing.T) {
	validTxResult := map[string]any{
		"slot": testSlot,
		"transaction": map[string]any{
			"message": map[string]any{
				"accountKeys": []string{"payer111", testSourceAddress},
				"instructions": []map[string]any{
					{
						"programIdIndex": 1,
						"accounts":       []int{0},
						"data":           base58.Encode([]byte("payload")),
					},
				},
			},
		},
	}

	serverURL, msg, justification := setupTest(t, makeMockRPC(t, validTxResult))
	require.Equal(t, http.StatusOK, doVerify(t, serverURL, msg, justification))
}

func TestVerify_TransactionNotFound(t *testing.T) {
	// result: null → transaction not found
	serverURL, msg, justification := setupTest(t, makeMockRPC(t, nil))
	require.Equal(t, http.StatusBadRequest, doVerify(t, serverURL, msg, justification))
}

func TestVerify_SlotMismatch(t *testing.T) {
	wrongSlotResult := map[string]any{
		"slot": testSlot + 999, // wrong slot
		"transaction": map[string]any{
			"message": map[string]any{
				"accountKeys": []string{"payer111", testSourceAddress},
				"instructions": []map[string]any{
					{
						"programIdIndex": 1,
						"accounts":       []int{0},
						"data":           base58.Encode([]byte("payload")),
					},
				},
			},
		},
	}

	serverURL, msg, justification := setupTest(t, makeMockRPC(t, wrongSlotResult))
	require.Equal(t, http.StatusBadRequest, doVerify(t, serverURL, msg, justification))
}

func TestVerify_ProgramNotFound(t *testing.T) {
	noProgramResult := map[string]any{
		"slot": testSlot,
		"transaction": map[string]any{
			"message": map[string]any{
				"accountKeys": []string{"payer111", "SomeOtherProgram111111111111111111111111111"},
				"instructions": []map[string]any{
					{
						"programIdIndex": 1, // points to SomeOtherProgram, not testSourceAddress
						"accounts":       []int{0},
						"data":           base58.Encode([]byte("payload")),
					},
				},
			},
		},
	}

	serverURL, msg, justification := setupTest(t, makeMockRPC(t, noProgramResult))
	require.Equal(t, http.StatusBadRequest, doVerify(t, serverURL, msg, justification))
}

func TestVerify_PayloadMismatch(t *testing.T) {
	wrongPayloadResult := map[string]any{
		"slot": testSlot,
		"transaction": map[string]any{
			"message": map[string]any{
				"accountKeys": []string{"payer111", testSourceAddress},
				"instructions": []map[string]any{
					{
						"programIdIndex": 1,
						"accounts":       []int{0},
						"data":           base58.Encode([]byte("WRONG_PAYLOAD")), // mismatched payload
					},
				},
			},
		},
	}

	serverURL, msg, justification := setupTest(t, makeMockRPC(t, wrongPayloadResult))
	require.Equal(t, http.StatusBadRequest, doVerify(t, serverURL, msg, justification))
}
