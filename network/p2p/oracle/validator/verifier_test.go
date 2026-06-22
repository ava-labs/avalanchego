// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// buildWarpMessage constructs a warp UnsignedMessage whose payload is the
// ABI-encoded OracleMessage. This is what the relayer puts in the access list
// predicate and what OracleAdapter.sol hashes for its payload-binding check.
func buildWarpMessage(t *testing.T, msg *oracle.OracleMessage) *warp.UnsignedMessage {
	t.Helper()
	um, err := warp.NewUnsignedMessage(1, ids.GenerateTestID(), msg.Bytes())
	require.NoError(t, err)
	return um
}

func TestOracleVerifier_ValidPath(t *testing.T) {
	msg, err := oracle.NewOracleMessage("solana", "addr1", []byte{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)

	um := buildWarpMessage(t, msg)

	mock := &MockSidecarClient{}
	verifier := NewOracleVerifier(mock, AllowedSources{
		"solana": {"addr1": {}},
	})

	appErr := verifier.Verify(t.Context(), um, []byte("justification"))
	require.Nil(t, appErr)
	require.Len(t, mock.Calls, 1)
}

func TestOracleVerifier_SourceTypeNotAllowed(t *testing.T) {
	msg, err := oracle.NewOracleMessage("bitcoin", "addr1", []byte{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)

	um := buildWarpMessage(t, msg)

	mock := &MockSidecarClient{}
	verifier := NewOracleVerifier(mock, AllowedSources{
		"solana": {"addr1": {}},
	})

	appErr := verifier.Verify(t.Context(), um, nil)
	require.NotNil(t, appErr)
	require.Equal(t, errCodeVerify, appErr.Code)
	require.Empty(t, mock.Calls)
}

func TestOracleVerifier_SourceAddressNotAllowed(t *testing.T) {
	msg, err := oracle.NewOracleMessage("solana", "unknown-addr", []byte{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)

	um := buildWarpMessage(t, msg)

	mock := &MockSidecarClient{}
	verifier := NewOracleVerifier(mock, AllowedSources{
		"solana": {"addr1": {}}, // non-empty inner map, unknown-addr not present
	})

	appErr := verifier.Verify(t.Context(), um, nil)
	require.NotNil(t, appErr)
	require.Equal(t, errCodeVerify, appErr.Code)
	require.Empty(t, mock.Calls)
}

func TestOracleVerifier_EmptyInnerMapAllowsAllAddresses(t *testing.T) {
	msg, err := oracle.NewOracleMessage("solana", "any-addr-whatsoever", []byte{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)

	um := buildWarpMessage(t, msg)

	mock := &MockSidecarClient{}
	verifier := NewOracleVerifier(mock, AllowedSources{
		"solana": {}, // empty inner map → all addresses allowed
	})

	appErr := verifier.Verify(t.Context(), um, nil)
	require.Nil(t, appErr)
	require.Len(t, mock.Calls, 1)
}

func TestOracleVerifier_SidecarReturnsError(t *testing.T) {
	msg, err := oracle.NewOracleMessage("solana", "addr1", []byte{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)

	um := buildWarpMessage(t, msg)

	mock := &MockSidecarClient{
		VerifyF: func(_ context.Context, _ *oracle.OracleEvent) error {
			return errors.New("sidecar says no")
		},
	}
	verifier := NewOracleVerifier(mock, AllowedSources{
		"solana": {"addr1": {}},
	})

	appErr := verifier.Verify(t.Context(), um, nil)
	require.NotNil(t, appErr)
	require.Equal(t, errCodeVerify, appErr.Code)
	require.Len(t, mock.Calls, 1)
}

func TestOracleVerifier_BadWarpPayload(t *testing.T) {
	// Raw bytes that are not valid ABI-encoded OracleMessage.
	um, err := warp.NewUnsignedMessage(1, ids.GenerateTestID(), []byte("notvalidabi"))
	require.NoError(t, err)

	mock := &MockSidecarClient{}
	verifier := NewOracleVerifier(mock, AllowedSources{
		"solana": {},
	})

	appErr := verifier.Verify(t.Context(), um, nil)
	require.NotNil(t, appErr)
	require.Equal(t, errCodeParse, appErr.Code)
	require.Empty(t, mock.Calls)
}
