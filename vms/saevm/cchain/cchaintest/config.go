// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchaintest

import (
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// ChainConfig returns a new C-Chain config with all network upgrades active.
func ChainConfig() *params.ChainConfig {
	c := *saetest.ChainConfig()
	extra := cparams.ChainConfig{
		NetworkUpgrades: cparams.NetworkUpgrades{
			ApricotPhase1Timestamp:     new(uint64),
			ApricotPhase2Timestamp:     new(uint64),
			ApricotPhase3Timestamp:     new(uint64),
			ApricotPhase4Timestamp:     new(uint64),
			ApricotPhase5Timestamp:     new(uint64),
			ApricotPhasePre6Timestamp:  new(uint64),
			ApricotPhase6Timestamp:     new(uint64),
			ApricotPhasePost6Timestamp: new(uint64),
			BanffTimestamp:             new(uint64),
			CortinaTimestamp:           new(uint64),
			DurangoTimestamp:           new(uint64),
			EtnaTimestamp:              new(uint64),
			FortunaTimestamp:           new(uint64),
			GraniteTimestamp:           new(uint64),
			HeliconTimestamp:           new(uint64),
			IglooTimestamp:             new(uint64),
		},
	}
	return cparams.WithExtra(&c, &extra)
}
