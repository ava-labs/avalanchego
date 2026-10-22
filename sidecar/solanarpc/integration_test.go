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
	"strings"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

// newVerifier is a test helper that constructs a SolanaVerifier from a plain
// RPC URL, marshaling it into the Config shape the verifier expects.
func newVerifier(t *testing.T, rpcURL string) *SolanaVerifier {
	t.Helper()
	cfgBytes, err := json.Marshal(Config{RPCURL: rpcURL})
	require.NoError(t, err)
	v, err := NewSolanaVerifier(cfgBytes)
	require.NoError(t, err)
	return v
}

// The Solana Memo Program exists in two deployed versions, both live on mainnet
// and devnet, and both are a reliable source of real on-chain data.
//
// Which one is useful depends on the network, so tests accept either. Sampling
// 30 recent transactions per address on mainnet:
//
//	memo v1: invoked as a top-level instruction in 30/30
//	memo v2: invoked as a top-level instruction in 2/30 — the other 28 merely
//	         pull v2 into the key space via an address lookup table
//
// On devnet v2 is the one that shows up as a top-level instruction. Searching
// both is what keeps these tests meaningful on mainnet and devnet alike.
const (
	memoV1Program = "Memo1UhkJRfHyvLMcVucJwxXeuD728EqVDDwQDxFMNo"
	memoV2Program = "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr"
)

var memoPrograms = []string{memoV1Program, memoV2Program}

// atokenProgram is the Associated Token Account Program. Almost every DeFi
// wallet interaction creates ATAs, so it reliably produces CPI transactions
// (it calls the Token Program and System Program via CPI).
const atokenProgram = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"

// fetchSignatures fetches up to limit recent transaction signatures that
// *mention* address, via getSignaturesForAddress.
//
// Despite the method name, this has nothing to do with signing authority:
// programs never sign transactions. The RPC indexes address mentions, so a
// transaction is returned whenever address appears anywhere in its key space —
// as an invoked program, as a passive account, or merely as an entry loaded
// through an address lookup table and never invoked at all. Callers that need
// the address to have actually been invoked must check the instructions
// themselves.
//
// Returns only the signature strings (errors from the RPC are fatal).
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
		// Public endpoints throttle aggressively. That is a property of the
		// endpoint, not a defect in the verifier, so skip rather than fail.
		if strings.Contains(strings.ToLower(result.Error.Message), "rate limit") {
			t.Skipf("SOLANA_RPC_URL is rate limiting (%s) — retry later or use a dedicated endpoint", result.Error.Message)
		}
		t.Fatalf("getSignaturesForAddress RPC error: %s", result.Error.Message)
	}

	sigs := make([]string, 0, len(result.Result))
	for _, r := range result.Result {
		sigs = append(sigs, r.Signature)
	}
	return sigs
}

// findMemoTxWithTopLevelInstruction scans recent transactions mentioning either
// Memo Program version and returns the first that actually invokes one as a
// top-level instruction: the signature, the transaction, the invoked program
// address, and that instruction's decoded data.
//
// Two filters matter here, and dropping either produces a test that fails for
// reasons unrelated to the verifier:
//
//   - Invocation. getSignaturesForAddress returns mere mentions, and on mainnet
//     memo v2 is usually only pulled in by a lookup table, never invoked. Taking
//     the newest mention therefore finds no Memo instruction most of the time.
//   - Success. Roughly half of recent memo transactions failed on-chain. A
//     failed transaction still lists its instructions, but its effects were
//     rolled back, so it is not a sound basis for asserting a verifier accepts
//     a real event.
func findMemoTxWithTopLevelInstruction(t *testing.T, rpcURL string) (string, *txResult, string, []byte) {
	t.Helper()
	client := newSolanaClient(rpcURL)
	for _, memoProgram := range memoPrograms {
		sigs := fetchSignatures(t, rpcURL, memoProgram, 50)
		for _, sig := range sigs {
			tx, err := client.getTransaction(t.Context(), sig)
			if err != nil || tx == nil {
				continue
			}
			if tx.Meta.failed() {
				continue
			}
			keys := buildEffectiveKeys(tx)
			for _, instr := range tx.Transaction.Message.Instructions {
				if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
					continue
				}
				if keys[instr.ProgramIDIndex] != memoProgram {
					continue
				}
				data, decErr := base58.Decode(instr.Data)
				if decErr != nil {
					continue
				}
				return sig, tx, memoProgram, data
			}
		}
	}
	t.Skip("no successful transaction invoking either Memo Program as a top-level instruction found in the 50 most recent mentions of each")
	return "", nil, "", nil
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
// one that has at least one inner instruction with decodeable data. Returns the
// signature and parsed txResult. Skips the test if none found.
//
// The "has a usable inner instruction" predicate is exactly what
// findFirstCPIInstruction answers, so this delegates rather than repeating the
// group/instruction walk — keeping the search and the subsequent extraction in
// TestSolanaVerifierIntegration_CPI guaranteed to agree.
func findTxWithInnerInstructions(t *testing.T, rpcURL string) (string, *txResult) {
	t.Helper()
	client := newSolanaClient(rpcURL)
	sigs := fetchSignatures(t, rpcURL, atokenProgram, 50)
	if len(sigs) == 0 {
		t.Skip("no recent AToken Program transactions — try again later or use a busier endpoint")
	}
	for _, sig := range sigs {
		tx, err := client.getTransaction(t.Context(), sig)
		if err != nil || tx == nil {
			continue
		}
		if tx.Meta.failed() {
			continue
		}
		if _, _, ok := findFirstCPIInstruction(tx); ok {
			return sig, tx
		}
	}
	t.Skip("no AToken Program transaction with inner-instruction data found in recent 50 signatures")
	return "", nil
}

// findV0TxWithLoadedAddresses searches recent Memo Program transactions for a
// successful one that has non-empty meta.loadedAddresses. Returns the signature
// and txResult. Skips the test if none found.
//
// Scans memo v2 specifically: on mainnet it is predominantly referenced *by*
// lookup tables, which is exactly the shape this test needs.
func findV0TxWithLoadedAddresses(t *testing.T, rpcURL string) (string, *txResult) {
	t.Helper()
	client := newSolanaClient(rpcURL)
	sigs := fetchSignatures(t, rpcURL, memoV2Program, 100)
	if len(sigs) == 0 {
		t.Skip("no recent Memo Program transactions — try again later or use a busier endpoint")
	}
	for _, sig := range sigs {
		tx, err := client.getTransaction(t.Context(), sig)
		if err != nil || tx == nil {
			continue
		}
		if tx.Meta.failed() {
			continue
		}
		if len(tx.Meta.LoadedAddresses.Writable)+len(tx.Meta.LoadedAddresses.Readonly) > 0 {
			return sig, tx
		}
	}
	t.Skip("no v0 Memo Program transaction with loadedAddresses found in recent 100 signatures — this is expected on quiet endpoints")
	return "", nil
}

// tamperedPayload returns a payload that differs from payload and that no
// instruction invoking program carries anywhere in tx.
//
// Simply flipping a byte is not enough. Programs are frequently invoked several
// times in one transaction with short payloads — a Token Program instruction
// discriminator is often a single byte — so a flipped payload can collide with a
// sibling invocation's real data. Verify would then correctly find that sibling
// and succeed, and the assertion that tampering is rejected would fail for a
// reason having nothing to do with tampering.
func tamperedPayload(tx *txResult, program string, payload []byte) []byte {
	keys := buildEffectiveKeys(tx)
	existing := make(map[string]struct{})
	collect := func(instrs []txInstruction) {
		for _, instr := range instrs {
			if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
				continue
			}
			if keys[instr.ProgramIDIndex] != program {
				continue
			}
			data, err := base58.Decode(instr.Data)
			if err != nil {
				continue
			}
			existing[string(data)] = struct{}{}
		}
	}
	collect(tx.Transaction.Message.Instructions)
	for _, group := range tx.Meta.InnerInstructions {
		collect(group.Instructions)
	}

	tampered := make([]byte, max(len(payload), 1))
	copy(tampered, payload)
	tampered[len(tampered)-1] ^= 0xFF
	for {
		if _, ok := existing[string(tampered)]; !ok {
			return tampered
		}
		tampered = append(tampered, 0xFF)
	}
}

// absentProgramAddress returns a syntactically valid Solana address that does
// not appear anywhere in tx's key space, so Verify is guaranteed to reach its
// "no instruction found" path for it.
func absentProgramAddress(tx *txResult) string {
	present := make(map[string]struct{})
	for _, k := range buildEffectiveKeys(tx) {
		present[k] = struct{}{}
	}
	candidate := make([]byte, 32)
	for i := range candidate {
		candidate[i] = 0x01
	}
	for {
		addr := base58.Encode(candidate)
		if _, ok := present[addr]; !ok {
			return addr
		}
		candidate[0]++
	}
}

// findUninvokedLoadedAddress returns the first meta.loadedAddresses entry that
// is not referenced as the program of any instruction, top-level or inner.
// Returns "" if every loaded address is invoked.
func findUninvokedLoadedAddress(tx *txResult) string {
	keys := buildEffectiveKeys(tx)
	invoked := make(map[string]struct{})
	markInvoked := func(instrs []txInstruction) {
		for _, instr := range instrs {
			if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
				continue
			}
			invoked[keys[instr.ProgramIDIndex]] = struct{}{}
		}
	}
	markInvoked(tx.Transaction.Message.Instructions)
	for _, group := range tx.Meta.InnerInstructions {
		markInvoked(group.Instructions)
	}

	loaded := append(append([]string(nil), tx.Meta.LoadedAddresses.Writable...), tx.Meta.LoadedAddresses.Readonly...)
	for _, addr := range loaded {
		if _, ok := invoked[addr]; !ok {
			return addr
		}
	}
	return ""
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

	// The helper guarantees the returned transaction invokes the Memo Program as
	// a top-level instruction, and returns that instruction's data as the ground
	// truth payload.
	txSig, tx, memoProgram, instrData := findMemoTxWithTopLevelInstruction(t, rpcURL)
	t.Logf("using transaction: %s", txSig)

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
		tampered := tamperedPayload(tx, memoProgram, instrData)
		msg, err := oracle.NewOracleMessage("solana", memoProgram, common.Address{}, slot, 1, tampered)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.Errorf(t, verifyErr, "expected payload mismatch error")
		require.Contains(t, verifyErr.Error(), "payload mismatch")
	})

	t.Run("wrong program rejected", func(t *testing.T) {
		// Derive an address absent from this transaction rather than hardcoding
		// one. A fixed "obviously unrelated" program does not work: mainnet memo
		// transactions routinely invoke the System Program too, in which case
		// Verify finds it and reports a payload mismatch, silently testing a
		// different code path than the one intended here.
		absentProgram := absentProgramAddress(tx)
		msg, err := oracle.NewOracleMessage("solana", absentProgram, common.Address{}, slot, 1, instrData)
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.ErrorIs(t, verifyErr, ErrInstructionNotFound)
		require.Contains(t, verifyErr.Error(), fmt.Sprintf("no instruction found for program %q", absentProgram))
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
		tampered := tamperedPayload(tx, programAddr, payload)
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
// Note: programs certainly can appear in loadedAddresses — on mainnet the Memo
// Program itself is frequently loaded through a lookup table (as a readonly
// entry) without being invoked. What this test does not attempt is a full
// happy path for a program that is both loaded via a lookup table and invoked;
// it validates key-space construction only.
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

	loaded := append(append([]string(nil), tx.Meta.LoadedAddresses.Writable...), tx.Meta.LoadedAddresses.Readonly...)
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
		// Pick a loaded address that no instruction actually invokes, rather than
		// assuming loaded[0] qualifies — lookup tables do carry programs, and an
		// invoked one would make Verify succeed and fail this assertion for the
		// wrong reason. Verify must return "no instruction found" for it, not
		// panic or index out of range.
		uninvokedAddr := findUninvokedLoadedAddress(tx)
		if uninvokedAddr == "" {
			t.Skip("every loaded address in this transaction is invoked by some instruction")
		}
		msg, err := oracle.NewOracleMessage("solana", uninvokedAddr, common.Address{}, slot, 1, []byte("anything"))
		require.NoError(t, err)
		verifyErr := verifier.Verify(t.Context(), msg, justification)
		require.ErrorIs(t, verifyErr, ErrInstructionNotFound)
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
