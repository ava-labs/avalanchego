// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

// stubRPC is a test double for rpcClient. Set Tx to return a transaction,
// Err to simulate an RPC failure, or leave both nil for "not found".
type stubRPC struct {
	Tx  *txResult
	Err error
}

func (s *stubRPC) getTransaction(_ context.Context, _ string) (*txResult, error) {
	return s.Tx, s.Err
}

const (
	testProgram = "So11111111111111111111111111111111111111112"
	testSlotV   = uint64(100)
)

func makeMsg(t *testing.T, sourceAddr string, slot uint64, payload []byte) *oracle.OracleMessage { //nolint:unparam
	t.Helper()
	msg, err := oracle.NewOracleMessage("solana", sourceAddr, []byte{1, 2, 3}, slot, 1, payload)
	require.NoError(t, err)
	return msg
}

func makeValidTx(programAddr string, slot uint64, payload []byte) *txResult {
	return &txResult{
		Slot: slot,
		Transaction: txData{
			Message: txMessage{
				AccountKeys: []string{"payer111", programAddr},
				Instructions: []txInstruction{
					{
						ProgramIDIndex: 1,
						Data:           base58.Encode(payload),
					},
				},
			},
		},
	}
}

func TestSolanaVerifier(t *testing.T) {
	payload := []byte("payload")

	tests := []struct {
		name    string
		rpc     *stubRPC
		msg     func(*testing.T) *oracle.OracleMessage
		wantErr string
	}{
		{
			name: "valid transaction",
			rpc:  &stubRPC{Tx: makeValidTx(testProgram, testSlotV, payload)},
			msg:  func(t *testing.T) *oracle.OracleMessage { return makeMsg(t, testProgram, testSlotV, payload) },
		},
		{
			name:    "rpc error",
			rpc:     &stubRPC{Err: errors.New("connection refused")},
			msg:     func(t *testing.T) *oracle.OracleMessage { return makeMsg(t, testProgram, testSlotV, payload) },
			wantErr: "getTransaction RPC call failed",
		},
		{
			name:    "transaction not found",
			rpc:     &stubRPC{Tx: nil},
			msg:     func(t *testing.T) *oracle.OracleMessage { return makeMsg(t, testProgram, testSlotV, payload) },
			wantErr: "transaction not found",
		},
		{
			name:    "slot mismatch",
			rpc:     &stubRPC{Tx: makeValidTx(testProgram, testSlotV+999, payload)},
			msg:     func(t *testing.T) *oracle.OracleMessage { return makeMsg(t, testProgram, testSlotV, payload) },
			wantErr: "slot mismatch",
		},
		{
			name:    "program not in account keys",
			rpc:     &stubRPC{Tx: makeValidTx("SomeOtherProgram1111111111111111111111111111", testSlotV, payload)},
			msg:     func(t *testing.T) *oracle.OracleMessage { return makeMsg(t, testProgram, testSlotV, payload) },
			wantErr: "no instruction found for program",
		},
		{
			name:    "payload mismatch",
			rpc:     &stubRPC{Tx: makeValidTx(testProgram, testSlotV, []byte("WRONG"))},
			msg:     func(t *testing.T) *oracle.OracleMessage { return makeMsg(t, testProgram, testSlotV, payload) },
			wantErr: "payload mismatch",
		},
		{
			name: "empty allowed sources allows all addresses",
			rpc:  &stubRPC{Tx: makeValidTx(testProgram, testSlotV, payload)},
			msg:  func(t *testing.T) *oracle.OracleMessage { return makeMsg(t, testProgram, testSlotV, payload) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &SolanaVerifier{client: tt.rpc}
			justification := make([]byte, 64)
			err := v.Verify(t.Context(), tt.msg(t), justification)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Errorf(t, err, "expected error containing %q", tt.wantErr)
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
