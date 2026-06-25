// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/mr-tron/base58"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

// SolanaVerifier verifies OracleMessages by querying the Solana RPC.
type SolanaVerifier struct {
	client rpcClient
}

func NewSolanaVerifier(rpcURL string, httpClient *http.Client) *SolanaVerifier {
	return &SolanaVerifier{
		client: newSolanaClient(rpcURL, httpClient),
	}
}

// errProgramNotFound is returned by matchInstruction when no instruction in the
// set invokes msg.SourceAddress. It signals the caller to keep searching (e.g.
// in inner instructions). Any other non-nil return means the program was found
// but verification failed — the caller must not continue searching.
var errProgramNotFound = errors.New("program not found in instruction set")

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
	// Encode the raw signature bytes as base58 to use as the RPC lookup key.
	sig := base58.Encode(justification)

	tx, err := v.client.getTransaction(ctx, sig)
	if err != nil {
		return fmt.Errorf("getTransaction RPC call failed: %w", err)
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

	return fmt.Errorf("no instruction found for program %q in transaction", msg.SourceAddress)
}
