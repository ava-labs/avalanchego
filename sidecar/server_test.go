// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"

	pb "github.com/ava-labs/avalanchego/proto/pb/oracle"
)

// mockVerifier is a test double for oracleVerifier. It returns the configured
// error, allowing server_test.go to stay independent of any chain implementation.
type mockVerifier struct {
	err error
}

func (m *mockVerifier) Verify(_ context.Context, _ *oracle.OracleMessage, _ []byte) error {
	return m.err
}

// validMsgBytes returns ABI-encoded bytes for a well-formed OracleMessage,
// suitable as pb.VerifyRequest.MessageBytes in tests that reach the verifier.
func validMsgBytes(t *testing.T) []byte {
	t.Helper()
	msg, err := oracle.NewOracleMessage(oracle.SourceTypeSolana, "So11111111111111111111111111111111111111112", common.Address{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)
	return msg.Bytes()
}

// unsupportedMsgBytes returns ABI-encoded bytes for a message with a source
// type the test server has no verifier for.
func unsupportedMsgBytes(t *testing.T) []byte {
	t.Helper()
	msg, err := oracle.NewOracleMessage("bitcoin", "addr", common.Address{}, 100, 1, []byte("payload"))
	require.NoError(t, err)
	return msg.Bytes()
}

func TestServer_Verify(t *testing.T) {
	tests := []struct {
		name      string
		req       *pb.VerifyRequest
		verifiers map[string]oracleVerifier
		wantCode  codes.Code
	}{
		{
			name:      "invalid message bytes returns InvalidArgument",
			req:       &pb.VerifyRequest{MessageBytes: []byte("not-abi"), Justification: make([]byte, 64)},
			verifiers: map[string]oracleVerifier{oracle.SourceTypeSolana: &mockVerifier{}},
			wantCode:  codes.InvalidArgument,
		},
		{
			// Verifier wraps oracle.ErrSourceUnavailable when the source chain RPC
			// is unreachable; server maps this to codes.Unavailable.
			name:      "source unavailable returns Unavailable",
			req:       &pb.VerifyRequest{MessageBytes: validMsgBytes(t), Justification: make([]byte, 64)},
			verifiers: map[string]oracleVerifier{oracle.SourceTypeSolana: &mockVerifier{err: fmt.Errorf("%w: rpc failed", oracle.ErrSourceUnavailable)}},
			wantCode:  codes.Unavailable,
		},
		{
			name:      "verifier rejection returns InvalidArgument",
			req:       &pb.VerifyRequest{MessageBytes: validMsgBytes(t), Justification: make([]byte, 64)},
			verifiers: map[string]oracleVerifier{oracle.SourceTypeSolana: &mockVerifier{err: errors.New("slot mismatch: got 99, want 100")}},
			wantCode:  codes.InvalidArgument,
		},
		{
			name:      "valid request returns OK",
			req:       &pb.VerifyRequest{MessageBytes: validMsgBytes(t), Justification: make([]byte, 64)},
			verifiers: map[string]oracleVerifier{oracle.SourceTypeSolana: &mockVerifier{}},
			wantCode:  codes.OK,
		},
		{
			// SourceType not registered → InvalidArgument, no verifier call.
			name:      "unregistered source type returns InvalidArgument",
			req:       &pb.VerifyRequest{MessageBytes: unsupportedMsgBytes(t), Justification: make([]byte, 64)},
			verifiers: map[string]oracleVerifier{oracle.SourceTypeSolana: &mockVerifier{}},
			wantCode:  codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(tt.verifiers)
			resp, err := srv.Verify(t.Context(), tt.req)
			require.Equal(t, tt.wantCode, status.Code(err))
			if tt.wantCode == codes.OK {
				require.NotNil(t, resp)
				require.NoError(t, err)
			}
		})
	}
}

func TestServer_PayloadRoundtrip(t *testing.T) {
	want, err := oracle.NewOracleMessage(oracle.SourceTypeSolana, "So11111111111111111111111111111111111111112", common.Address{}, 100, 1, []byte("payload"))
	require.NoError(t, err)

	srv := NewServer(map[string]oracleVerifier{oracle.SourceTypeSolana: &mockVerifier{}})
	resp, err := srv.Verify(t.Context(), &pb.VerifyRequest{
		MessageBytes:  want.Bytes(),
		Justification: make([]byte, 64),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}
