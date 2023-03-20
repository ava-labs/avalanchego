// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	errTestingDropped = errors.New("testing dropped")
	preFundedKeys     = secp256k1.TestKeys()
)

func TestValidGossipedTxIsAddedToBuilder(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	builder := builder.NewMockBuilder(ctrl)

	// create a tx
	tx, err := createTestTx(0)
	require.NoError(err)
	builder.EXPECT().GetDropReason(tx.ID()).Return(nil).Times(1)

	msg := message.TxGossip{Tx: tx.Bytes()}
	// show that unknown tx is added to builder
	builder.EXPECT().AddUnverifiedTx(tx).Times(1)
	gossipHandler := NewGossipHandler(snow.DefaultContextTest(), builder)

	err = gossipHandler.HandleTxGossip(ids.GenerateTestNodeID(), &msg)
	require.NoError(err)
}

func TestInvalidTxIsNotAddedToBuilder(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	builder := builder.NewMockBuilder(ctrl)

	// create a tx and mark as invalid
	tx, err := createTestTx(0)
	require.NoError(err)
	builder.EXPECT().GetDropReason(tx.ID()).Return(errTestingDropped).Times(1)

	gossipHandler := NewGossipHandler(snow.DefaultContextTest(), builder)
	msg := message.TxGossip{Tx: tx.Bytes()}
	// show that the invalid tx is not added to builder
	builder.EXPECT().AddUnverifiedTx(gomock.Any()).Times(0)
	// handle the tx gossip
	err = gossipHandler.HandleTxGossip(ids.GenerateTestNodeID(), &msg)
	require.NoError(err)
}

func createTestTx(index int) (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{
		TxID: ids.ID{'r', 'e', 'w', 'a', 'r', 'd', 'I', 'D'},
	}
	signers := [][]*secp256k1.PrivateKey{{preFundedKeys[index]}}
	return txs.NewSigned(utx, txs.Codec, signers)
}
