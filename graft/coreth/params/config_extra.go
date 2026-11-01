// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"

	ethparams "github.com/ava-labs/libevm/params"
)

func GetExtra(c *ethparams.ChainConfig) *cparams.ChainConfig {
	ex := payloads.ChainConfig.Get(c)
	if ex == nil {
		ex = &cparams.ChainConfig{}
		payloads.ChainConfig.Set(c, ex)
	}
	return ex
}

// WithExtra sets the extra payload on `c` and returns the modified argument.
func WithExtra(c *ethparams.ChainConfig, extra *cparams.ChainConfig) *ethparams.ChainConfig {
	payloads.ChainConfig.Set(c, extra)
	return c
}
