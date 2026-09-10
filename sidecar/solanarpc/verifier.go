// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package solanarpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mr-tron/base58"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

const defaultRPCURL = "https://api.mainnet-beta.solana.com"

// Config is the verifier's own configuration, parsed from the sidecar's
// --config-path file. The sidecar binary treats this as opaque bytes;
// SolanaVerifier is the authority on what fields are valid.
type Config struct {
	// RPCURL is the Solana JSON-RPC endpoint.
	// Defaults to https://api.mainnet-beta.solana.com if omitted.
	RPCURL string `json:"rpc_url"`
	// AllowedPrograms is an optional list of Solana program addresses this
	// sidecar will attest to. Omit or leave empty to allow all programs.
	AllowedPrograms []string `json:"allowed_programs"`
}

// SolanaVerifier verifies OracleMessages by querying the Solana RPC.
type SolanaVerifier struct {
	client          rpcClient
	allowedPrograms map[string]struct{} // empty = allow all
}

// NewSolanaVerifier parses configBytes as a JSON Config and constructs a
// SolanaVerifier. configBytes may be nil or empty, in which case defaults
// apply (mainnet RPC, all programs allowed).
func NewSolanaVerifier(configBytes []byte) (*SolanaVerifier, error) {
	cfg := Config{RPCURL: defaultRPCURL}
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			return nil, fmt.Errorf("invalid solana verifier config: %w", err)
		}
	}
	if cfg.RPCURL == "" {
		return nil, errors.New("solana verifier config: rpc_url must not be empty")
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedPrograms))
	for _, p := range cfg.AllowedPrograms {
		allowed[p] = struct{}{}
	}
	return &SolanaVerifier{
		client:          newSolanaClient(cfg.RPCURL),
		allowedPrograms: allowed,
	}, nil
}

// errProgramNotFound is returned by matchInstruction when no instruction in the
// set invokes msg.SourceAddress.
//
// No non-nil result from matchInstruction is decisive on its own: a program can
// be invoked several times within one transaction, both at the top level and
// through CPI, so the caller must examine every instruction set before
// concluding that verification failed. Only a nil return ends the search.
var errProgramNotFound = errors.New("program not found in instruction set")

// errPayloadMismatch means msg.SourceAddress was invoked in this instruction
// set, but no invocation of it carried data equal to msg.Payload.
var errPayloadMismatch = errors.New("payload mismatch: instruction data does not match OracleMessage.Payload")

// ErrInstructionNotFound is returned by Verify when no instruction in the
// transaction (including CPI inner instructions) invokes msg.SourceAddress.
var ErrInstructionNotFound = errors.New("no instruction found for program")

// matchInstruction scans instrs for an instruction whose program matches
// msg.SourceAddress and whose data matches msg.Payload. Returns nil on the
// first full match, errProgramNotFound if the program is absent from the set,
// or errPayloadMismatch if it was invoked but never with the claimed payload.
//
// Every invocation of the program is examined, not just the first. A program
// invoked repeatedly in one transaction — a batch of memos, a multi-hop swap —
// produces several instructions with the same programId and different data, and
// each is an independently attestable event. Returning on the first programId
// hit would make every invocation after the first unverifiable.
func matchInstruction(instrs []txInstruction, keys []string, msg *oracle.OracleMessage) error {
	// Least-specific result, upgraded as more is learned about why no match was
	// found. Never downgraded, so the most informative reason survives.
	result := errProgramNotFound
	for _, instr := range instrs {
		if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
			continue
		}
		if keys[instr.ProgramIDIndex] != msg.SourceAddress {
			continue
		}
		data, err := base58.Decode(instr.Data)
		if err != nil {
			// Malformed data from the RPC must not mask a valid invocation
			// elsewhere in the set, so record it and keep scanning.
			if errors.Is(result, errProgramNotFound) {
				result = fmt.Errorf("failed to base58-decode instruction data: %w", err)
			}
			continue
		}
		if !bytes.Equal(data, msg.Payload) {
			if errors.Is(result, errProgramNotFound) {
				result = errPayloadMismatch
			}
			continue
		}
		return nil
	}
	return result
}

// Verify checks that the given OracleMessage is backed by a real Solana
// transaction. The justification must be a raw 64-byte Ed25519 transaction
// signature.
func (v *SolanaVerifier) Verify(ctx context.Context, msg *oracle.OracleMessage, justification []byte) error {
	if len(v.allowedPrograms) > 0 {
		if _, ok := v.allowedPrograms[msg.SourceAddress]; !ok {
			return fmt.Errorf("source address %q is not in the allowed programs list", msg.SourceAddress)
		}
	}

	// Encode the raw signature bytes as base58 to use as the RPC lookup key.
	sig := base58.Encode(justification)

	tx, err := v.client.getTransaction(ctx, sig)
	if err != nil {
		return fmt.Errorf("%w: getTransaction RPC call failed: %w", oracle.ErrSourceUnavailable, err)
	}
	if tx == nil {
		return fmt.Errorf("transaction not found for signature %s", sig)
	}

	// 1. Reject transactions that failed on-chain. A failed transaction is still
	// recorded with its full instruction list, but the runtime rolled its effects
	// back, so nothing it describes actually happened. Attesting to one would
	// have validators sign for an event that never took effect.
	if tx.Meta.failed() {
		return fmt.Errorf("transaction %s failed on-chain and cannot be attested: %s", sig, tx.Meta.Err)
	}

	// 2. Verify the slot matches the claimed source block height.
	if tx.Slot != msg.SourceBlockHeight {
		return fmt.Errorf("slot mismatch: got %d, want %d", tx.Slot, msg.SourceBlockHeight)
	}

	// Build the effective account key space. For legacy transactions
	// loadedAddresses is empty; for v0 transactions it contains accounts
	// resolved from address lookup tables that programIdIndex may reference.
	// Copied rather than appended onto AccountKeys, whose backing array append
	// would otherwise write into when its capacity exceeds its length.
	keys := make([]string, 0, len(tx.Transaction.Message.AccountKeys)+len(tx.Meta.LoadedAddresses.Writable)+len(tx.Meta.LoadedAddresses.Readonly))
	keys = append(keys, tx.Transaction.Message.AccountKeys...)
	keys = append(keys, tx.Meta.LoadedAddresses.Writable...)
	keys = append(keys, tx.Meta.LoadedAddresses.Readonly...)

	// 3. Find an instruction that invokes msg.SourceAddress with data equal to
	// msg.Payload, checking top-level instructions and then each CPI inner
	// instruction group.
	//
	// Every set is searched before failing. The same program may be invoked in
	// more than one set — top-level with one payload, via CPI with another — so
	// a non-match in one set says nothing about the rest. Only a match ends the
	// search early.
	result := matchInstruction(tx.Transaction.Message.Instructions, keys, msg)
	if result == nil {
		return nil
	}
	for _, group := range tx.Meta.InnerInstructions {
		err := matchInstruction(group.Instructions, keys, msg)
		if err == nil {
			return nil
		}
		// Upgrade to the more informative reason, never back to "not found".
		if errors.Is(result, errProgramNotFound) {
			result = err
		}
	}

	if errors.Is(result, errProgramNotFound) {
		return fmt.Errorf("%w %q in transaction", ErrInstructionNotFound, msg.SourceAddress)
	}
	return result
}
