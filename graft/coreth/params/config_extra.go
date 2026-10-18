// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import "github.com/ava-labs/avalanchego/graft/coreth/params/extras"

const (
	// TODO: Value to pass to geth's Rules by default where the appropriate
	// context is not available in the avalanche code. (similar to context.TODO())
	IsMergeTODO = true
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
