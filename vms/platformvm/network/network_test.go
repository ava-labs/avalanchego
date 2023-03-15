// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestRecentTxNotRegossiped(t *testing.T) {
	require := require.New(t)

	sender := &common.SenderTest{T: t}
	network := NewNetwork(snow.DefaultContextTest(), sender, message.NoopGossipHandler{})

	testTx, err := newTestTx(0)

	require.NoError(err)

	called := new(int)
	sender.SendAppGossipF = func(_ context.Context, b []byte) error {
		*called++
		return nil
	}

	err = network.GossipTx(testTx)
	require.NoError(err)

	err = network.GossipTx(testTx)
	require.NoError(err)

	require.Equal(1, *called)

	// Test that a different tx will be gossiped
	testTx2, err := newTestTx(1)
	require.NoError(err)

	err = network.GossipTx(testTx2)
	require.NoError(err)

	require.Equal(2, *called)
}

func newTestTx(keyIndex int) (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{
		TxID: ids.ID{'r', 'e', 'w', 'a', 'r', 'd', 'I', 'D'},
	}

	signers := [][]*secp256k1.PrivateKey{{secp256k1.TestKeys()[keyIndex]}}
	testTx, err := txs.NewSigned(utx, txs.Codec, signers)

	return testTx, err
}
