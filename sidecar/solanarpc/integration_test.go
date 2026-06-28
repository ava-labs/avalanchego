// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package solanarpc

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

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

// newVerifier is a test helper that constructs a SolanaVerifier from a plain
// RPC URL, marshaling it into the Config shape the verifier expects.
func newVerifier(t *testing.T, rpcURL string) *SolanaVerifier {
	t.Helper()
	cfgBytes, err := json.Marshal(Config{RPCURL: rpcURL})
	require.NoError(t, err)
	v, err := NewSolanaVerifier(cfgBytes, nil)
	require.NoError(t, err)
	return v
}

// memoProgram is the Solana Memo Program v2 address. It exists on both mainnet
// and devnet and almost always has recent transactions, making it a reliable
// source of real on-chain data for integration testing.
const memoProgram = "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr"

// atokenProgram is the Associated Token Account Program. Almost every DeFi
// wallet interaction creates ATAs, so it reliably produces CPI transactions
// (it calls the Token Program and System Program via CPI).
const atokenProgram = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJe8bXh"

// fetchSignatures fetches up to limit recent transaction signatures for address
// from rpcURL. Returns only the signature strings (errors from the RPC are fatal).
func fetchSignatures(t *testing.T, rpcURL, address string, limit int) []string {
	t.Helper()

	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "getSignaturesForAddress",
		"params":  []any{address, map[string]any{"limit": limit}},
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

	sigs := make([]string, 0, len(result.Result))
	for _, r := range result.Result {
		sigs = append(sigs, r.Signature)
	}
	return sigs
}

// fetchRecentMemoTxSig fetches the most recent transaction signature that
// involved the Memo Program from the given Solana RPC endpoint.
func fetchRecentMemoTxSig(t *testing.T, rpcURL string) string {
	t.Helper()
	sigs := fetchSignatures(t, rpcURL, memoProgram, 1)
	if len(sigs) == 0 {
		t.Skip("no recent Memo Program transactions found at SOLANA_RPC_URL — try again later or use a busier endpoint")
	}
	return sigs[0]
}

// buildEffectiveKeys mirrors the key-space construction in verifier.go so that
// test helpers and verifier.Verify stay consistent as the code evolves.
func buildEffectiveKeys(tx *txResult) []string {
	keys := make([]string, len(tx.Transaction.Message.AccountKeys))
	copy(keys, tx.Transaction.Message.AccountKeys)
	keys = append(keys, tx.Meta.LoadedAddresses.Writable...)
	keys = append(keys, tx.Meta.LoadedAddresses.Readonly...)
	return keys
}

// findTxWithInnerInstructions searches recent AToken Program transactions for
// one that has at least one inner instruction group with usable data. Returns
// the signature and parsed txResult. Skips the test if none found.
func findTxWithInnerInstructions(t *testing.T, rpcURL string) (string, *txResult) {
	t.Helper()
	client := newSolanaClient(rpcURL, nil)
	sigs := fetchSignatures(t, rpcURL, atokenProgram, 50)
	if len(sigs) == 0 {
		t.Skip("no recent AToken Program transactions — try again later or use a busier endpoint")
	}
	for _, sig := range sigs {
		tx, err := client.getTransaction(t.Context(), sig)
		if err != nil || tx == nil {
			continue
		}
		if len(tx.Meta.InnerInstructions) == 0 {
			continue
		}
		// Require at least one inner instruction with decodeable data.
		keys := buildEffectiveKeys(tx)
		for _, group := range tx.Meta.InnerInstructions {
			for _, instr := range group.Instructions {
				if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
					continue
				}
				if _, decErr := base58.Decode(instr.Data); decErr == nil {
					return sig, tx
				}
			}
		}
	}
	t.Skip("no AToken Program transaction with inner-instruction data found in recent 50 signatures")
	return "", nil
}

// findV0TxWithLoadedAddresses searches recent Memo Program transactions for one
// that has non-empty meta.loadedAddresses. Returns the signature and txResult.
// Skips the test if none found.
func findV0TxWithLoadedAddresses(t *testing.T, rpcURL string) (string, *txResult) {
	t.Helper()
	client := newSolanaClient(rpcURL, nil)
	sigs := fetchSignatures(t, rpcURL, memoProgram, 100)
	if len(sigs) == 0 {
		t.Skip("no recent Memo Program transactions — try again later or use a busier endpoint")
	}
	for _, sig := range sigs {
		tx, err := client.getTransaction(t.Context(), sig)
		if err != nil || tx == nil {
			continue
		}
		if len(tx.Meta.LoadedAddresses.Writable)+len(tx.Meta.LoadedAddresses.Readonly) > 0 {
			return sig, tx
		}
	}
	t.Skip("no v0 Memo Program transaction with loadedAddresses found in recent 100 signatures — this is expected on quiet endpoints")
	return "", nil
}

// findFirstCPIInstruction returns the program address and decoded payload of the
// first inner instruction with decodeable data. ok is false if none found.
func findFirstCPIInstruction(tx *txResult) (programAddr string, payload []byte, ok bool) {
	keys := buildEffectiveKeys(tx)
	for _, group := range tx.Meta.InnerInstructions {
		for _, instr := range group.Instructions {
			if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
				continue
			}
			data, err := base58.Decode(instr.Data)
			if err != nil {
				continue
			}
			return keys[instr.ProgramIDIndex], data, true
		}
	}
	return "", nil, false
}

// TestSolanaVerifierIntegration tests SolanaVerifier against a real Solana RPC
// for the basic happy path (Memo Program, top-level instruction).
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

	verifier := newVerifier(t, rpcURL)

	t.Run("valid transaction accepted", func(t *testing.T) {
		msg, err := oracle.NewOracleMessage("solana", memoProgram, common.Address{}, slot, 1, instrData)
		require.NoError(t, err)
		require.NoError(t, verifier.Verify(t.Context(), msg, justification))
	})

	t.Run("slot off by one rejected", func(t *testing.T) {
		msg, err := oracle.NewOracleMessage("solana", memoProgram, common.Address{}, slot+1, 1, instrData)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Errorf(t, verifyErr, "expected slot mismatch error")
		require.Contains(t, verifyErr.Error(), "slot mismatch")
	})

	t.Run("payload tampered rejected", func(t *testing.T) {
		tampered := make([]byte, max(len(instrData), 1))
		copy(tampered, instrData)
		tampered[len(tampered)-1] ^= 0xFF
		msg, err := oracle.NewOracleMessage("solana", memoProgram, common.Address{}, slot, 1, tampered)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Errorf(t, verifyErr, "expected payload mismatch error")
		require.Contains(t, verifyErr.Error(), "payload mismatch")
	})

	t.Run("wrong program rejected", func(t *testing.T) {
		const systemProgram = "11111111111111111111111111111111"
		msg, err := oracle.NewOracleMessage("solana", systemProgram, common.Address{}, slot, 1, instrData)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Errorf(t, verifyErr, "expected program-not-found error")
		require.Contains(t, verifyErr.Error(), fmt.Sprintf("no instruction found for program %q", systemProgram))
	})
}

// TestSolanaVerifierIntegration_CPI verifies that SolanaVerifier correctly finds
// programs invoked via Cross-Program Invocation (CPI), which appear in
// meta.innerInstructions rather than transaction.message.instructions.
//
// Uses the Associated Token Account Program, which reliably produces CPI calls
// into the Token Program and System Program on every ATA creation.
//
// Requires SOLANA_RPC_URL. Skips automatically if no qualifying transaction is
// found in the 50 most recent AToken Program transactions.
func TestSolanaVerifierIntegration_CPI(t *testing.T) {
	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if rpcURL == "" {
		t.Skip("SOLANA_RPC_URL not set")
	}

	txSig, tx := findTxWithInnerInstructions(t, rpcURL)
	t.Logf("using CPI transaction: %s", txSig)

	programAddr, payload, ok := findFirstCPIInstruction(tx)
	require.True(t, ok, "findTxWithInnerInstructions should have guaranteed at least one decodeable inner instruction")

	slot := tx.Slot
	justification, err := base58.Decode(txSig)
	require.NoError(t, err)

	verifier := newVerifier(t, rpcURL)

	t.Run("CPI instruction accepted", func(t *testing.T) {
		msg, err := oracle.NewOracleMessage("solana", programAddr, common.Address{}, slot, 1, payload)
		require.NoError(t, err)
		require.NoError(t, verifier.Verify(t.Context(), msg, justification))
	})

	t.Run("slot off by one rejected", func(t *testing.T) {
		msg, err := oracle.NewOracleMessage("solana", programAddr, common.Address{}, slot+1, 1, payload)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Errorf(t, verifyErr, "expected slot mismatch error")
		require.Contains(t, verifyErr.Error(), "slot mismatch")
	})

	t.Run("payload tampered rejected", func(t *testing.T) {
		tampered := make([]byte, max(len(payload), 1))
		copy(tampered, payload)
		tampered[len(tampered)-1] ^= 0xFF
		msg, err := oracle.NewOracleMessage("solana", programAddr, common.Address{}, slot, 1, tampered)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Errorf(t, verifyErr, "expected payload mismatch error")
		require.Contains(t, verifyErr.Error(), "payload mismatch")
	})
}

// TestSolanaVerifierIntegration_V0LoadedAddresses verifies that SolanaVerifier
// correctly parses meta.loadedAddresses from v0 transactions and builds the
// combined key space (static accountKeys + loadedAddresses).
//
// Two things are validated:
//  1. The struct parsing works: we can read non-empty loadedAddresses from a
//     real v0 transaction (proves rpc.go decodes the JSON correctly).
//  2. Key space correctness: a loaded address that is NOT referenced as a
//     programIdIndex in any instruction causes "no instruction found", not a
//     panic or index-out-of-bounds — confirming the key slice is built right.
//
// Note: it is essentially impossible in practice for a program to be IN
// loadedAddresses (lookup tables hold data accounts, not programs). The test
// therefore only validates key-space construction, not a full CPI-via-lookup
// happy path.
//
// Requires SOLANA_RPC_URL. Skips automatically if no qualifying v0 transaction
// is found in the 100 most recent Memo Program transactions.
func TestSolanaVerifierIntegration_V0LoadedAddresses(t *testing.T) {
	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if rpcURL == "" {
		t.Skip("SOLANA_RPC_URL not set")
	}

	txSig, tx := findV0TxWithLoadedAddresses(t, rpcURL)
	t.Logf("using v0 transaction: %s", txSig)

	loaded := append(tx.Meta.LoadedAddresses.Writable, tx.Meta.LoadedAddresses.Readonly...)
	require.NotEmpty(t, loaded, "findV0TxWithLoadedAddresses should have guaranteed non-empty loadedAddresses")

	t.Logf("loadedAddresses: writable=%d readonly=%d",
		len(tx.Meta.LoadedAddresses.Writable),
		len(tx.Meta.LoadedAddresses.Readonly),
	)

	slot := tx.Slot
	justification, err := base58.Decode(txSig)
	require.NoError(t, err)

	verifier := newVerifier(t, rpcURL)

	t.Run("loaded address not invoked returns no-instruction-found", func(t *testing.T) {
		// Use the first loaded address as the claimed program. It is not
		// referenced by any programIdIndex, so Verify must return the
		// "no instruction found" error — not panic or index error.
		uninvokedAddr := loaded[0]
		msg, err := oracle.NewOracleMessage("solana", uninvokedAddr, common.Address{}, slot, 1, []byte("anything"))
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Error(t, verifyErr)
		require.Contains(t, verifyErr.Error(), "no instruction found for program")
	})

	t.Run("top-level instruction on v0 transaction accepted", func(t *testing.T) {
		// Find a top-level instruction that we can verify to confirm the verifier
		// works end-to-end on this v0 transaction (not just for the error case).
		keys := buildEffectiveKeys(tx)
		var foundProgram string
		var foundPayload []byte
		for _, instr := range tx.Transaction.Message.Instructions {
			if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
				continue
			}
			data, decErr := base58.Decode(instr.Data)
			if decErr != nil {
				continue
			}
			foundProgram = keys[instr.ProgramIDIndex]
			foundPayload = data
			break
		}
		if foundProgram == "" {
			t.Skip("no decodeable top-level instruction found in this v0 transaction")
		}
		msg, err := oracle.NewOracleMessage("solana", foundProgram, common.Address{}, slot, 1, foundPayload)
		require.NoError(t, err)
		require.NoError(t, verifier.Verify(t.Context(), msg, justification))
	})
}
