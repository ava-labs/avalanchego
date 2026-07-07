// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package solanarpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mr-tron/base58"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

const defaultRPCURL = "https://api.mainnet-beta.solana.com"

// Config is the verifier's own configuration, parsed from the sidecar's
// --config file. The sidecar binary treats this as opaque bytes; SolanaVerifier
// is the authority on what fields are valid.
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
func NewSolanaVerifier(configBytes []byte, httpClient *http.Client) (*SolanaVerifier, error) {
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
		client:          newSolanaClient(cfg.RPCURL, httpClient),
		allowedPrograms: allowed,
	}, nil
}

// errProgramNotFound is returned by matchInstruction when no instruction in the
// set invokes msg.SourceAddress. It signals the caller to keep searching (e.g.
// in inner instructions). Any other non-nil return means the program was found
// but verification failed — the caller must not continue searching.
var errProgramNotFound = errors.New("program not found in instruction set")

// ErrInstructionNotFound is returned by Verify when no instruction in the
// transaction (including CPI inner instructions) invokes msg.SourceAddress.
var ErrInstructionNotFound = errors.New("no instruction found for program")

// matchInstruction scans instrs for one whose program matches msg.SourceAddress
// and whose data matches msg.Payload. Returns nil on the first match,
// errProgramNotFound if the program isn't present, or a descriptive error if
// the program is present but the data doesn't verify.
func matchInstruction(instrs []txInstruction, keys []string, msg *oracle.OracleMessage) error {
	for _, instr := range instrs {
		if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
			continue
		}
		if keys[instr.ProgramIDIndex] != msg.SourceAddress {
			continue
		}
		data, err := base58.Decode(instr.Data)
		if err != nil {
			return fmt.Errorf("failed to base58-decode instruction data: %w", err)
		}
		if !bytes.Equal(data, msg.Payload) {
			return errors.New("payload mismatch: instruction data does not match OracleMessage.Payload")
		}
		return nil
	}
	return errProgramNotFound
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

	// 1. Verify the slot matches the claimed source block height.
	if tx.Slot != msg.SourceBlockHeight {
		return fmt.Errorf("slot mismatch: got %d, want %d", tx.Slot, msg.SourceBlockHeight)
	}

	// Build the effective account key space. For legacy transactions
	// loadedAddresses is empty; for v0 transactions it contains accounts
	// resolved from address lookup tables that programIdIndex may reference.
	keys := tx.Transaction.Message.AccountKeys
	keys = append(keys, tx.Meta.LoadedAddresses.Writable...)
	keys = append(keys, tx.Meta.LoadedAddresses.Readonly...)

	// 2. Find an instruction whose programId matches msg.SourceAddress.
	// 3. For that instruction, verify the decoded data equals msg.Payload.
	// Check top-level instructions first, then CPI inner instructions.
	// Only continue to the next set if the program was not found; a
	// definitive failure (e.g. payload mismatch) stops the search immediately.
	if err := matchInstruction(tx.Transaction.Message.Instructions, keys, msg); !errors.Is(err, errProgramNotFound) {
		return err
	}
	for _, group := range tx.Meta.InnerInstructions {
		if err := matchInstruction(group.Instructions, keys, msg); !errors.Is(err, errProgramNotFound) {
			return err
		}
	}

	return fmt.Errorf("%w %q in transaction", ErrInstructionNotFound, msg.SourceAddress)
}
