// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchaintest

import (
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
)

// ChainConfig returns a new C-Chain config with all network upgrades active.
func ChainConfig() *params.ChainConfig {
	c := *saetest.ChainConfig()
	extra := extras.ChainConfig{
		NetworkUpgrades: extras.NetworkUpgrades{
			ApricotPhase1BlockTimestamp:     new(uint64),
			ApricotPhase2BlockTimestamp:     new(uint64),
			ApricotPhase3BlockTimestamp:     new(uint64),
			ApricotPhase4BlockTimestamp:     new(uint64),
			ApricotPhase5BlockTimestamp:     new(uint64),
			ApricotPhasePre6BlockTimestamp:  new(uint64),
			ApricotPhase6BlockTimestamp:     new(uint64),
			ApricotPhasePost6BlockTimestamp: new(uint64),
			BanffBlockTimestamp:             new(uint64),
			CortinaBlockTimestamp:           new(uint64),
			DurangoBlockTimestamp:           new(uint64),
			EtnaTimestamp:                   new(uint64),
			FortunaTimestamp:                new(uint64),
			GraniteTimestamp:                new(uint64),
			HeliconTimestamp:                new(uint64),
			IglooTimestamp:                  new(uint64),
		},
	}
	return corethparams.WithExtra(&c, &extra)
}
