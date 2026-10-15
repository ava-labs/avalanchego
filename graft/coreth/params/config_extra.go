// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
)

const (
	// TODO: Value to pass to geth's Rules by default where the appropriate
	// context is not available in the avalanche code. (similar to context.TODO())
	IsMergeTODO = true
)

// TestFixtureChainID identifies the pre-SAE fixture chain used for SAE testing
// of synchronous blocks. Like Fuji and Mainnet, its Berlin and London heights
// are pinned so that it can cross AP2 and AP3 mid-chain.
var TestFixtureChainID = big.NewInt(43110)

// Berlin and London activation heights of the [TestFixtureChainID] chain.
const (
	TestFixtureBerlinBlock = 3
	TestFixtureLondonBlock = 5
)

func GetExtra(c *ChainConfig) *extras.ChainConfig {
	ex := payloads.ChainConfig.Get(c)
	if ex == nil {
		ex = &extras.ChainConfig{}
		payloads.ChainConfig.Set(c, ex)
	}
	return ex
}

// WithExtra sets the extra payload on `c` and returns the modified argument.
func WithExtra(c *ChainConfig, extra *extras.ChainConfig) *ChainConfig {
	payloads.ChainConfig.Set(c, extra)
	return c
}
