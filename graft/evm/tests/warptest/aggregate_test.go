// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warptest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestAggregateSignatures(t *testing.T) {
	require := require.New(t)
	network := &tmpnet.Network{
		Nodes: tmpnet.NewNodesOrPanic(3),
	}

	validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, 2)
	for i, node := range network.Nodes[:2] {
		pop, err := node.GetProofOfPossession()
		require.NoError(err, "node.GetProofOfPossession()")
		validatorSet[node.NodeID] = &validators.GetValidatorOutput{
			NodeID:    node.NodeID,
			PublicKey: pop.Key(),
			Weight:    uint64(i + 1),
		}
	}
	warpSet, err := validators.FlattenValidatorSet(validatorSet)
	require.NoError(err, "validators.FlattenValidatorSet()")

	unsignedMessage, err := warp.NewUnsignedMessage(1, ids.GenerateTestID(), []byte("payload"))
	require.NoError(err, "warp.NewUnsignedMessage()")
	signedMessage, err := AggregateSignatures(network, warpSet, unsignedMessage)
	require.NoError(err, "AggregateSignatures()")

	require.Equal(unsignedMessage.Bytes(), signedMessage.UnsignedMessage.Bytes(), "AggregateSignatures() unsigned message")
	numSigners, err := signedMessage.Signature.NumSigners()
	require.NoError(err, "signedMessage.Signature.NumSigners()")
	require.Equal(len(validatorSet), numSigners, "AggregateSignatures() signer count")
	require.NoError(
		signedMessage.Signature.Verify(unsignedMessage, unsignedMessage.NetworkID, warpSet, 1, 1),
		"AggregateSignatures() signature verification",
	)
}
