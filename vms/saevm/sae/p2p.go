// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"time"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/prometheus/client_golang/prometheus"
)

// newNetwork creates the P2P network with a registered validator set.
func newNetwork(
	snowCtx *snow.Context,
	sender common.AppSender,
	reg *prometheus.Registry,
) (
	*p2p.Network,
	*p2p.Peers,
	*p2p.Validators,
	error,
) {
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
		return nil, nil, nil, err
	}
	return network, peers, validatorPeers, nil
}
