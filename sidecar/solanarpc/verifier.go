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

	// 2. Find an instruction whose programId matches msg.SourceAddress.
	// 3. For that instruction, verify the decoded data equals msg.Payload.
	keys := tx.Transaction.Message.AccountKeys
	for _, instr := range tx.Transaction.Message.Instructions {
		if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
			continue
		}
		if keys[instr.ProgramIDIndex] != msg.SourceAddress {
			continue
		}
		// Found the matching program; decode and compare the data.
		data, err := base58.Decode(instr.Data)
		if err != nil {
			return fmt.Errorf("failed to base58-decode instruction data: %w", err)
		}
		if !bytes.Equal(data, msg.Payload) {
			return errors.New("payload mismatch: instruction data does not match OracleMessage.Payload")
		}
		return nil
	}

	return fmt.Errorf("no instruction found for program %q in transaction", msg.SourceAddress)
}
