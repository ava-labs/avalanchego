// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

// memoProgram is the Solana Memo Program v2 address. It exists on both mainnet
// and devnet and almost always has recent transactions, making it a reliable
// source of real on-chain data for integration testing.
const memoProgram = "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr"

// fetchRecentMemoTxSig fetches the most recent transaction signature that
// involved the Memo Program from the given Solana RPC endpoint.
func fetchRecentMemoTxSig(t *testing.T, rpcURL string) string {
	t.Helper()

	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "getSignaturesForAddress",
		"params":  []any{memoProgram, map[string]any{"limit": 1}},
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rpcURL, bytes.NewReader(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result struct {
		Result []struct {
			Signature string `json:"signature"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	if result.Error != nil {
		t.Fatalf("getSignaturesForAddress RPC error: %s", result.Error.Message)
	}
	if len(result.Result) == 0 {
		t.Skip("no recent Memo Program transactions found at SOLANA_RPC_URL — try again later or use a busier endpoint")
	}
	return result.Result[0].Signature
}

// TestSolanaVerifierIntegration tests SolanaVerifier against a real Solana RPC.
//
// Required environment variable:
//
//	SOLANA_RPC_URL — e.g. https://api.devnet.solana.com or https://api.mainnet-beta.solana.com
//
// The test auto-discovers a recent Memo Program transaction; no transaction
// signature needs to be supplied manually.
func TestSolanaVerifierIntegration(t *testing.T) {
	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if rpcURL == "" {
		t.Skip("SOLANA_RPC_URL not set")
	}

	txSig := fetchRecentMemoTxSig(t, rpcURL)
	t.Logf("using transaction: %s", txSig)

	// Fetch the full transaction to extract ground truth (slot, program, payload).
	client := newSolanaClient(rpcURL, nil)
	tx, err := client.getTransaction(t.Context(), txSig)
	require.NoError(t, err)
	require.NotNil(t, tx, "transaction not found — the signature returned by getSignaturesForAddress was not retrievable")

	instrs := tx.Transaction.Message.Instructions
	keys := tx.Transaction.Message.AccountKeys
	require.NotEmpty(t, instrs)

	// Find the Memo Program instruction and use its data as ground truth payload.
	var instrData []byte
	for _, instr := range instrs {
		if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
			continue
		}
		if keys[instr.ProgramIDIndex] != memoProgram {
			continue
		}
		data, decodeErr := base58.Decode(instr.Data)
		if decodeErr != nil {
			continue
		}
		instrData = data
		break
	}
	require.NotNil(t, instrData, "could not find Memo Program instruction in transaction")

	slot := tx.Slot
	justification, err := base58.Decode(txSig)
	require.NoError(t, err)

	verifier := NewSolanaVerifier(rpcURL, nil)

	t.Run("valid transaction accepted", func(t *testing.T) {
		msg, err := oracle.NewOracleMessage("solana", memoProgram, []byte{0}, slot, 1, instrData)
		require.NoError(t, err)
		require.NoError(t, verifier.Verify(t.Context(), msg, justification))
	})

	t.Run("slot off by one rejected", func(t *testing.T) {
		msg, err := oracle.NewOracleMessage("solana", memoProgram, []byte{0}, slot+1, 1, instrData)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Errorf(t, verifyErr, "expected slot mismatch error")
		require.Contains(t, verifyErr.Error(), "slot mismatch")
	})

	t.Run("payload tampered rejected", func(t *testing.T) {
		tampered := make([]byte, max(len(instrData), 1))
		copy(tampered, instrData)
		tampered[len(tampered)-1] ^= 0xFF
		msg, err := oracle.NewOracleMessage("solana", memoProgram, []byte{0}, slot, 1, tampered)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Errorf(t, verifyErr, "expected payload mismatch error")
		require.Contains(t, verifyErr.Error(), "payload mismatch")
	})

	t.Run("wrong program rejected", func(t *testing.T) {
		const systemProgram = "11111111111111111111111111111111"
		msg, err := oracle.NewOracleMessage("solana", systemProgram, []byte{0}, slot, 1, instrData)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Errorf(t, verifyErr, "expected program-not-found error")
		require.Contains(t, verifyErr.Error(), fmt.Sprintf("no instruction found for program %q", systemProgram))
	})
}
