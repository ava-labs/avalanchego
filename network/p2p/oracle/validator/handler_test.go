// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const testNetworkID = uint32(123)

var testChainID = ids.ID{1, 2, 3}

// buildHandlerRequest constructs a serialized sdk.SignatureRequest for the
// given OracleMessage using fixed networkID/chainID so that the warp.Signer
// created with the same values can sign the message without a chain ID mismatch.
func buildHandlerRequest(t *testing.T, msg *oracle.OracleMessage) (requestBytes []byte, um *warp.UnsignedMessage) {
	t.Helper()
	ac, err := payload.NewAddressedCall(nil, msg.Bytes())
	require.NoError(t, err)
	um, err = warp.NewUnsignedMessage(testNetworkID, testChainID, ac.Bytes())
	require.NoError(t, err)
	b, err := proto.Marshal(&sdk.SignatureRequest{
		Message:       um.Bytes(),
		Justification: []byte("test-justification"),
	})
	require.NoError(t, err)
	return b, um
}

// TestOracleHandler_HappyPath sends a valid oracle signature request through
// the full CachedHandler → OracleVerifier → MockSidecarClient stack and
// verifies the returned BLS signature is correct.
func TestOracleHandler_HappyPath(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	sk, err := localsigner.New()
	require.NoError(err)
	pk := sk.PublicKey()

	signer := warp.NewSigner(sk, testNetworkID, testChainID)

	mock := &MockSidecarClient{}
	verifier := NewOracleVerifier(mock)
	handler := acp118.NewCachedHandler(&cache.Empty[ids.ID, []byte]{}, verifier, signer)

	clientNodeID := ids.GenerateTestNodeID()
	serverNodeID := ids.GenerateTestNodeID()
	c := p2ptest.NewClient(t, ctx, clientNodeID, p2p.NoOpHandler{}, serverNodeID, handler)

	msg, err := oracle.NewOracleMessage("solana", "addr1", common.Address{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(err)
	requestBytes, um := buildHandlerRequest(t, msg)

	handled := make(chan struct{})
	onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, appErr error) {
		defer func() { handled <- struct{}{} }()
		require.NoError(appErr)

		response := &sdk.SignatureResponse{}
		require.NoError(proto.Unmarshal(responseBytes, response))

		sig, err := bls.SignatureFromBytes(response.Signature)
		require.NoError(err)
		require.True(bls.Verify(pk, sig, um.Bytes()))
	}

	require.NoError(c.AppRequest(ctx, set.Of(serverNodeID), requestBytes, onResponse))
	<-handled

	require.Len(mock.Calls, 1)
}

// TestOracleHandler_CacheHit sends the same message twice. The sidecar is
// configured to fail on the second call. The second response must still
// succeed — served from the LRU cache, never reaching the sidecar again.
func TestOracleHandler_CacheHit(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()

	sk, err := localsigner.New()
	require.NoError(err)
	pk := sk.PublicKey()

	signer := warp.NewSigner(sk, testNetworkID, testChainID)

	callCount := 0
	mock := &MockSidecarClient{
		VerifyF: func(_ context.Context, _ *oracle.OracleEvent) error {
			callCount++
			if callCount > 1 {
				return errors.New("sidecar must not be called after the first signature is cached")
			}
			return nil
		},
	}

	verifier := NewOracleVerifier(mock)
	handler := acp118.NewCachedHandler(lru.NewCache[ids.ID, []byte](4), verifier, signer)

	clientNodeID := ids.GenerateTestNodeID()
	serverNodeID := ids.GenerateTestNodeID()
	c := p2ptest.NewClient(t, ctx, clientNodeID, p2p.NoOpHandler{}, serverNodeID, handler)

	msg, err := oracle.NewOracleMessage("solana", "addr1", common.Address{0xAB}, 42, 7, []byte("data"))
	require.NoError(err)
	requestBytes, um := buildHandlerRequest(t, msg)

	for i := range 2 {
		handled := make(chan struct{})
		onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, appErr error) {
			defer func() { handled <- struct{}{} }()
			require.NoErrorf(appErr, "request %d failed unexpectedly", i)

			response := &sdk.SignatureResponse{}
			require.NoError(proto.Unmarshal(responseBytes, response))
			sig, err := bls.SignatureFromBytes(response.Signature)
			require.NoError(err)
			require.True(bls.Verify(pk, sig, um.Bytes()))
		}
		require.NoError(c.AppRequest(ctx, set.Of(serverNodeID), requestBytes, onResponse))
		<-handled
	}

	require.Equal(1, callCount, "sidecar called more than once; cache miss on repeated request")
}
