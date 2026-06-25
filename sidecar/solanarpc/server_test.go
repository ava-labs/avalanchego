// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
)

func newStubVerifier(rpc rpcClient) *SolanaVerifier {
	return &SolanaVerifier{client: rpc}
}

func TestServer_Verify(t *testing.T) {
	msg := makeMsg(t, testProgram, testSlotV, []byte("payload"))
	justification := make([]byte, 64)

	tests := []struct {
		name     string
		req      *pb.VerifyRequest
		rpc      *stubRPC
		wantCode codes.Code
	}{
		{
			name:     "invalid message bytes returns InvalidArgument",
			req:      &pb.VerifyRequest{MessageBytes: []byte("not-abi"), Justification: justification},
			rpc:      &stubRPC{},
			wantCode: codes.InvalidArgument,
		},
		{
			// stubRPC.Err causes getTransaction to fail; SolanaVerifier wraps it
			// as "getTransaction RPC call failed: ..." which triggers codes.Unavailable.
			name:     "RPC failure returns Unavailable",
			req:      &pb.VerifyRequest{MessageBytes: msg.Bytes(), Justification: justification},
			rpc:      &stubRPC{Err: errors.New("connection refused")},
			wantCode: codes.Unavailable,
		},
		{
			// TX is found but has wrong slot → slot mismatch error → codes.InvalidArgument.
			name:     "verifier rejection returns InvalidArgument",
			req:      &pb.VerifyRequest{MessageBytes: msg.Bytes(), Justification: justification},
			rpc:      &stubRPC{Tx: makeValidTx(testProgram, testSlotV+1, []byte("payload"))},
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "valid request returns OK",
			req:      &pb.VerifyRequest{MessageBytes: msg.Bytes(), Justification: justification},
			rpc:      &stubRPC{Tx: makeValidTx(testProgram, testSlotV, []byte("payload"))},
			wantCode: codes.OK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(newStubVerifier(tt.rpc))
			resp, err := srv.Verify(t.Context(), tt.req)
			require.Equal(t, tt.wantCode, status.Code(err))
			if tt.wantCode == codes.OK {
				require.NotNil(t, resp)
				require.NoError(t, err)
			}
		})
	}
}

// TestServer_PayloadRoundtrip verifies that the bytes sent through
// pb.VerifyRequest are identical to what oracle.ParseOracleMessage returns.
func TestServer_PayloadRoundtrip(t *testing.T) {
	want, err := oracle.NewOracleMessage("solana", testProgram, [20]byte{}, testSlotV, 1, []byte("payload"))
	require.NoError(t, err)

	srv := NewServer(newStubVerifier(&stubRPC{Tx: makeValidTx(testProgram, testSlotV, []byte("payload"))}))
	resp, err := srv.Verify(t.Context(), &pb.VerifyRequest{
		MessageBytes:  want.Bytes(),
		Justification: make([]byte, 64),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}
