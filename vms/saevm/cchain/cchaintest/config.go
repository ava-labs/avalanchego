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
	extra := *extras.TestIglooChainConfig
	return corethparams.WithExtra(&c, &extra)
}
