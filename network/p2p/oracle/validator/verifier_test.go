// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

// buildWarpMessage constructs a warp UnsignedMessage with the canonical
// AddressedCall{SourceAddress: nil, Payload: abi.encode(OracleMessage)} structure.
// This mirrors what the relayer/aggregator must produce for the warp precompile
// to accept the message on the destination chain.
func buildWarpMessage(t *testing.T, msg *oracle.OracleMessage) *warp.UnsignedMessage {
	t.Helper()
	ac, err := payload.NewAddressedCall(nil, msg.Bytes())
	require.NoError(t, err)
	um, err := warp.NewUnsignedMessage(1, ids.GenerateTestID(), ac.Bytes())
	require.NoError(t, err)
	return um
}

// TestSignatureRequestHandlerID asserts that oracle attestation is registered at
// handler ID 4, distinct from the native warp handler ID (2). This constant is
// the shared contract between validators and icm-services' OracleSignatureAggregator.
func TestSignatureRequestHandlerID(t *testing.T) {
	require.Equal(t, uint64(4), SignatureRequestHandlerID)
}

func TestOracleVerifier_ValidPath(t *testing.T) {
	msg, err := oracle.NewOracleMessage("solana", "addr1", common.Address{1, 2, 3}, 100, 1, []byte("payload"))
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
	msg, err := oracle.NewOracleMessage("bitcoin", "addr1", common.Address{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)

	um := buildWarpMessage(t, msg)

	mock := &MockSidecarClient{}
	verifier := NewOracleVerifier(mock, AllowedSources{
		"solana": {"addr1": {}},
	})

	appErr := verifier.Verify(t.Context(), um, nil)
	require.NotNil(t, appErr)
	require.Equal(t, errCodeAllowlist, appErr.Code)
	require.Empty(t, mock.Calls)
}

func TestOracleVerifier_SourceAddressNotAllowed(t *testing.T) {
	msg, err := oracle.NewOracleMessage("solana", "unknown-addr", common.Address{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)

	um := buildWarpMessage(t, msg)

	mock := &MockSidecarClient{}
	verifier := NewOracleVerifier(mock, AllowedSources{
		"solana": {"addr1": {}}, // non-empty inner map, unknown-addr not present
	})

	appErr := verifier.Verify(t.Context(), um, nil)
	require.NotNil(t, appErr)
	require.Equal(t, errCodeAllowlist, appErr.Code)
	require.Empty(t, mock.Calls)
}

func TestOracleVerifier_EmptyInnerMapAllowsAllAddresses(t *testing.T) {
	msg, err := oracle.NewOracleMessage("solana", "any-addr-whatsoever", common.Address{1, 2, 3}, 100, 1, []byte("payload"))
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
	msg, err := oracle.NewOracleMessage("solana", "addr1", common.Address{1, 2, 3}, 100, 1, []byte("payload"))
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
