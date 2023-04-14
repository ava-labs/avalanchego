// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var _ validators.SetCallbackListener = (*StakerListener)(nil)

type StakerListener struct {
	SubnetID ids.ID
	Router   Router
	Subnet   subnets.Subnet
}

func (s *StakerListener) OnValidatorAdded(
	nodeID ids.NodeID,
	_ *bls.PublicKey,
	txID ids.ID,
	_ uint64,
) {
	for _, chainID := range s.Subnet.GetChains() {
		msg := message.InternalStaked(nodeID, chainID, txID)
		// TODO make sure these aren't dropped in the handler, similar to
		//  connected and disconnected
		s.Router.HandleInbound(context.TODO(), msg)
	}
}

func (s *StakerListener) OnValidatorRemoved(
	nodeID ids.NodeID,
	txID ids.ID,
	_ uint64,
) {
	for _, chainID := range s.Subnet.GetChains() {
		msg := message.InternalUnstaked(nodeID, chainID, txID)
		s.Router.HandleInbound(context.TODO(), msg)
	}
}

func (*StakerListener) OnValidatorWeightChanged(ids.NodeID, uint64, uint64) {}
