// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var (
	_ validators.Connector = (*Network)(nil)
	_ common.AppHandler    = (*Network)(nil)
)

type Network struct {
	*p2p.Network
	peers          *p2p.Peers
	validatorPeers *p2p.Validators
}

// NewNetwork creates the P2P network with a registered validator set.
func NewNetwork(
	snowCtx *snow.Context,
	sender common.AppSender,
	reg *prometheus.Registry,
) (Network, error) {
	peers := &p2p.Peers{}
	const maxValidatorSetStaleness = time.Minute
	validatorPeers := p2p.NewValidators(
		snowCtx.Log,
		snowCtx.SubnetID,
		snowCtx.ValidatorState,
		maxValidatorSetStaleness,
	)
	const namespace = "p2p"
	network, err := p2p.NewNetwork(
		snowCtx.Log,
		sender,
		reg,
		namespace,
		peers,
		validatorPeers,
	)
	if err != nil {
		return Network{}, err
	}
	return Network{
		Network:        network,
		peers:          peers,
		validatorPeers: validatorPeers,
	}, nil
}
