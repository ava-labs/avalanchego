// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package external_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/external"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// mockSidecarClient is a hand-rolled SidecarClient for testing.
type mockSidecarClient struct {
	err error
}

func (m *mockSidecarClient) Verify(_ context.Context, _ *external.ExternalEvent) error {
	return m.err
}

// helpers

const (
	testNetworkID uint32 = 1
	testChainType        = "solana"
	testAddress          = "SomeProgram1111111111111111111111111111111111"
)

var testChainID = ids.GenerateTestID()

func buildExternalWarpMessage(t *testing.T, msg *external.ExternalMessage) *avalancheWarp.UnsignedMessage {
	t.Helper()
	ac, err := payload.NewAddressedCall(nil, msg.Bytes())
	require.NoError(t, err)
	warpMsg, err := avalancheWarp.NewUnsignedMessage(testNetworkID, testChainID, ac.Bytes())
	require.NoError(t, err)
	return warpMsg
}

func buildValidMessage(t *testing.T) *avalancheWarp.UnsignedMessage {
	t.Helper()
	msg, err := external.NewExternalMessage(
		testChainType,
		testAddress,
		make([]byte, 20),
		42_000,
		[]byte("hello from solana"),
	)
	require.NoError(t, err)
	return buildExternalWarpMessage(t, msg)
}

// ExternalChainVerifier tests

func TestVerifier_InvalidOuterPayload(t *testing.T) {
	v := external.NewExternalChainVerifier(&mockSidecarClient{}, external.AllowedSources{
		testChainType: {},
	})
	// Payload bytes that cannot be parsed as any warp payload.
	warpMsg, err := avalancheWarp.NewUnsignedMessage(testNetworkID, testChainID, []byte("not a valid payload"))
	require.NoError(t, err)

	appErr := v.Verify(t.Context(), warpMsg, nil)
	require.NotNil(t, appErr)
	require.Equal(t, external.ParseErrCode, appErr.Code)
}

func TestVerifier_NonAddressedCallPayload(t *testing.T) {
	v := external.NewExternalChainVerifier(&mockSidecarClient{}, external.AllowedSources{
		testChainType: {},
	})
	// Hash payload, not AddressedCall.
	hashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(t, err)
	warpMsg, err := avalancheWarp.NewUnsignedMessage(testNetworkID, testChainID, hashPayload.Bytes())
	require.NoError(t, err)

	appErr := v.Verify(t.Context(), warpMsg, nil)
	require.NotNil(t, appErr)
	require.Equal(t, external.ParseErrCode, appErr.Code)
}

func TestVerifier_NonExternalMessagePayload(t *testing.T) {
	v := external.NewExternalChainVerifier(&mockSidecarClient{}, external.AllowedSources{
		testChainType: {},
	})
	// AddressedCall with inner bytes that are not an ExternalMessage.
	ac, err := payload.NewAddressedCall(nil, []byte("not an external message"))
	require.NoError(t, err)
	warpMsg, err := avalancheWarp.NewUnsignedMessage(testNetworkID, testChainID, ac.Bytes())
	require.NoError(t, err)

	appErr := v.Verify(t.Context(), warpMsg, nil)
	require.NotNil(t, appErr)
	require.Equal(t, external.ParseErrCode, appErr.Code)
}

func TestVerifier_ChainTypeNotAllowed(t *testing.T) {
	v := external.NewExternalChainVerifier(&mockSidecarClient{}, external.AllowedSources{
		// "solana" is not registered.
	})
	warpMsg := buildValidMessage(t)

	appErr := v.Verify(t.Context(), warpMsg, nil)
	require.NotNil(t, appErr)
	require.Equal(t, external.VerifyErrCode, appErr.Code)
}

func TestVerifier_SourceAddressNotAllowed(t *testing.T) {
	v := external.NewExternalChainVerifier(&mockSidecarClient{}, external.AllowedSources{
		testChainType: {
			"OtherProgram": {},
		},
	})
	warpMsg := buildValidMessage(t)

	appErr := v.Verify(t.Context(), warpMsg, nil)
	require.NotNil(t, appErr)
	require.Equal(t, external.VerifyErrCode, appErr.Code)
}

func TestVerifier_AllSourceAddressesAllowed(t *testing.T) {
	// Empty inner map means all source addresses are permitted.
	sidecar := &mockSidecarClient{}
	v := external.NewExternalChainVerifier(sidecar, external.AllowedSources{
		testChainType: {},
	})
	warpMsg := buildValidMessage(t)

	appErr := v.Verify(t.Context(), warpMsg, nil)
	require.Nil(t, appErr)
}

func TestVerifier_SidecarFails(t *testing.T) {
	sidecar := &mockSidecarClient{err: errors.New("transaction not found")}
	v := external.NewExternalChainVerifier(sidecar, external.AllowedSources{
		testChainType: {testAddress: {}},
	})
	warpMsg := buildValidMessage(t)

	appErr := v.Verify(t.Context(), warpMsg, nil)
	require.NotNil(t, appErr)
	require.Equal(t, external.VerifyErrCode, appErr.Code)
}

func TestVerifier_SidecarPasses(t *testing.T) {
	sidecar := &mockSidecarClient{}
	v := external.NewExternalChainVerifier(sidecar, external.AllowedSources{
		testChainType: {testAddress: {}},
	})
	warpMsg := buildValidMessage(t)

	appErr := v.Verify(t.Context(), warpMsg, nil)
	require.Nil(t, appErr)
}

func TestVerifier_JustificationPassedToSidecar(t *testing.T) {
	var capturedEvent *external.ExternalEvent
	sidecar := &captureSidecarClient{capture: &capturedEvent}
	v := external.NewExternalChainVerifier(sidecar, external.AllowedSources{
		testChainType: {testAddress: {}},
	})
	warpMsg := buildValidMessage(t)
	justification := []byte("solana-tx-sig-abc123")

	appErr := v.Verify(t.Context(), warpMsg, justification)
	require.Nil(t, appErr)
	require.NotNil(t, capturedEvent)
	require.Equal(t, justification, capturedEvent.Justification)
}

type captureSidecarClient struct {
	capture **external.ExternalEvent
}

func (c *captureSidecarClient) Verify(_ context.Context, event *external.ExternalEvent) error {
	*c.capture = event
	return nil
}
